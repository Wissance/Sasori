package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/wissance/Ferrum/globals"
	"github.com/wissance/Ferrum/swagger"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/wissance/Ferrum/api/rest"
	"github.com/wissance/Ferrum/config"
	"github.com/wissance/Ferrum/data"
	"github.com/wissance/Ferrum/logging"
	"github.com/wissance/Ferrum/managers"
	"github.com/wissance/Ferrum/services"
	r "github.com/wissance/gwuu/api/rest"
	"github.com/wissance/stringFormatter"
	"gopkg.in/natefinch/lumberjack.v2"
)

const ferrumSwaggerAddressEnvVariable = "FERRUM_SWAGGER_EXT_ADDRESS"

type Application struct {
	devMode            bool
	appConfigFile      *string
	dataConfigFile     *string
	secretKeyFile      *string
	appConfig          *config.AppConfig
	authenticationDefs *data.AuthenticationDefs
	secretKey          []byte
	serverData         *data.ServerData
	dataProvider       *managers.DataContext
	webApiHandler      *r.WebApiHandler
	webApiContext      *rest.WebApiContext
	logger             *logging.AppLogger
	httpHandler        *http.Handler
	httpServer         *http.Server
	shutdownTimeout    time.Duration
}

// CreateAppWithConfigs creates but not Init new Application as AppRunner
/* This function creates new Application and pass configFile to newly created object
 * Parameters:
 *     - configFile - path to config
 *     - devMode - developer mode (for showing some non production info = swagger, ...)
 * Returns: new Application as AppRunner
 */
func CreateAppWithConfigs(configFile string, devMode bool) AppRunner {
	app := &Application{}
	app.devMode = devMode
	app.appConfigFile = &configFile
	app.authenticationDefs = &data.AuthenticationDefs{}
	appRunner := AppRunner(app)
	return appRunner
}

// CreateAppWithData creates but not Init new Application as AppRunner
/* This function creates new Application and pass already decoded json of appConfig and serverData plus secretKey
 * Parameters:
 *     - appConfig  - decoded config
 *     - serverData - decoded server config
 *     - secretKey  - secret key that is using for signing JWT
 * Returns: new Application as AppRunner
 */
func CreateAppWithData(appConfig *config.AppConfig, serverData *data.ServerData, secretKey []byte, devMode bool) AppRunner {
	app := &Application{appConfig: appConfig, secretKey: secretKey, serverData: serverData, devMode: devMode}
	app.authenticationDefs = &data.AuthenticationDefs{}
	appRunner := AppRunner(app)
	return appRunner
}

// Start function that starts application
/* This function must be called after Init it starts application web server either on HTTP or HTTPS 9depends on config Schema value)
 * Parameters: no
 * Return start result (true if Start was successful) and error (nil if start was successful)
 */
func (app *Application) Start() (bool, error) {
	err := app.startWebService()
	if err != nil {
		app.logger.Error(stringFormatter.Format("An error occurred during API Service Start"))
	}
	return err == nil, err
}

// Init initializes application
/* This function implements application subsystem init:
 *    1. Read config if Application was not Created via CreateAppWithData
 *    2. After config was decoded this function implements following initialization
 *       2.1 Read secret file for signing JWT
 *       2.2 Initializes logger
 *       2.3 Initializes Data Provider
 *       2.4 Initializes REST API
 * Parameters: no
 * Return result of init (true if init was successful) and error (nil if init was successful)
 */
func (app *Application) Init() (bool, error) {
	// part that initializes app from configs
	isAppCreatedWithConfigs := app.appConfigFile != nil
	if isAppCreatedWithConfigs {
		// init logger
		cfg, err := config.ReadAppConfig(*app.appConfigFile)
		if err != nil {
			return false, fmt.Errorf("An error occurred during reading app config file: %w", err)
		}
		app.appConfig = cfg
		app.logger = logging.CreateLogger(&app.appConfig.Logging)
		app.logger.Init()

		// after config read init secretKey file and data file (if provider.type == FILE)
		app.secretKeyFile = &app.appConfig.ServerCfg.SecretFile
		if app.appConfig.DataSource.Type == config.FILE {
			app.dataConfigFile = &app.appConfig.DataSource.Source
		}
		// reading secrets key
		key := app.readKey()
		if key == nil {
			app.logger.Error("An error occurred during reading secret key, key is nil")
			return false, errors.New("secret key is nil")
		}
		app.secretKey = key
	} else {
		// init logger
		app.logger = logging.CreateLogger(&app.appConfig.Logging)
		app.logger.Init()
	}
	// common part: both configs and direct struct pass
	// init users, today we are reading data file
	err := app.initDataProviders()
	if err != nil {
		app.logger.Error(stringFormatter.Format("An error occurred during data providers init: {0}", err.Error()))
		return false, err
	}

	// init auth defs
	app.initAuthServerDefs()

	// init webapi
	err = app.initRestApi()
	if err != nil {
		app.logger.Error(stringFormatter.Format("An error occurred during rest api init: {0}", err.Error()))
		return false, err
	}

	app.httpServer = &http.Server{Handler: *app.httpHandler}
	app.shutdownTimeout = 5 * time.Second
	return true, nil
}

// Stop function that stops application
/* Now doesn't do anything, just a stub
 * Parameters : no
 * Returns result of app stop and error
 */
func (app *Application) Stop(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, app.shutdownTimeout)
	defer cancel()
	err := app.httpServer.Shutdown(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetLogger function that returns logger from initialized application
/* Returns logger from application (app.Logger), must be called after Init
 * Parameters : no
 * Returns: logger
 */
func (app *Application) GetLogger() *logging.AppLogger {
	return app.logger
}

func (app *Application) initDataProviders() error {
	var err error
	if app.serverData != nil {
		dataProvider, prepareErr := managers.PrepareContextUsingData(&app.appConfig.DataSource, app.serverData, app.logger)
		app.dataProvider = &dataProvider
		return prepareErr
	}

	if app.dataConfigFile != nil {
		dataProvider, prepareErr := managers.PrepareContextUsingFile(&app.appConfig.DataSource, app.dataConfigFile, app.logger)
		app.dataProvider = &dataProvider
		err = prepareErr
	} else {
		dataProvider, prepareErr := managers.PrepareContext(&app.appConfig.DataSource, app.logger)
		app.dataProvider = &dataProvider
		err = prepareErr
	}
	return err
}

func (app *Application) initRestApi() error {
	app.webApiHandler = r.NewWebApiHandler(true, r.AnyOrigin)
	securityService := services.CreateSecurityService(app.dataProvider, app.logger)
	serverAddress := stringFormatter.Format("{0}:{1}", app.appConfig.ServerCfg.Address, app.appConfig.ServerCfg.Port)
	app.webApiContext = &rest.WebApiContext{
		Address: serverAddress, Schema: string(app.appConfig.ServerCfg.Schema),
		AuthDefs:     app.authenticationDefs,
		DataProvider: app.dataProvider, Security: &securityService,
		TokenGenerator: &services.JwtGenerator{SignKey: app.secretKey, Logger: app.logger}, Logger: app.logger,
	}
	router := app.webApiHandler.Router
	router.StrictSlash(true)
	app.initKeyCloakSimilarRestApiRoutes(router)
	if app.devMode {
		app.initSwaggerRoutes(router)
	}
	// Setting up listener for logging
	appenderIndex := app.logger.GetAppenderIndex(config.RollingFile, app.appConfig.Logging.Appenders)
	if appenderIndex == -1 {
		app.logger.Info("The RollingFile appender was not found.")
		var resultRouter http.Handler = router
		app.httpHandler = &resultRouter
		return nil
	}
	app.httpHandler = app.createHttpLoggingHandler(appenderIndex, router)
	return nil
}

func (app *Application) initSwaggerRoutes(router *mux.Router) {
	// Swagger docs router and config
	swagger.SwaggerInfo.Version = "v0.9"
	swagger.SwaggerInfo.Title = "Ferrum Authorization Server"
	swagger.SwaggerInfo.Description = "Ferrum a better Authorization server compatible by API with a KeyCloak"
	address := app.getSwaggerAddress()
	if address == "" {
		address = app.appConfig.ServerCfg.Address
	}
	swagger.SwaggerInfo.Host = stringFormatter.Format("{0}:{1}", address, app.appConfig.ServerCfg.Port)

	router.PathPrefix("/swagger/").Handler(httpSwagger.Handler())
}

func (app *Application) initAuthServerDefs() {
	app.authenticationDefs.SupportedGrantTypes = []string{
		globals.AuthorizationTokenGrantType,
		globals.RefreshTokenGrantType,
		globals.PasswordGrantType,
	}

	app.authenticationDefs.SupportedResponseTypes = []string{
		globals.TokenResponseType,
		globals.CodeResponseType,
		globals.CodeTokenResponseType,
	}

	app.authenticationDefs.SupportedResponses = []string{
		globals.JwtResponse,
	}

	app.authenticationDefs.SupportedScopes = []string{
		globals.ProfileEmailScope,
		globals.OpenIdScope,
		globals.ProfileScope,
		globals.EmailScope,
	}

	app.authenticationDefs.SupportedClaimTypes = []string{
		"normal",
	}

	app.authenticationDefs.SupportedClaims = []string{
		globals.SubClaimType,
		globals.EmailClaimType,
		globals.PreferredUsernameClaim,
	}
}

func (app *Application) initKeyCloakSimilarRestApiRoutes(router *mux.Router) {
	// 1. Introspect endpoint - /auth/realms/{realm}/protocol/openid-connect/introspect
	app.webApiHandler.HandleFunc(router, "/auth/realms/{realm}/protocol/openid-connect/token/introspect", app.webApiContext.Introspect, http.MethodPost)
	app.webApiHandler.HandleFunc(router, "/realms/{realm}/protocol/openid-connect/token/introspect", app.webApiContext.Introspect, http.MethodPost)
	// 2. Generate token endpoint - /auth/realms/{realm}/protocol/openid-connect/token
	app.webApiHandler.HandleFunc(router, "/auth/realms/{realm}/protocol/openid-connect/token", app.webApiContext.IssueNewToken, http.MethodPost)
	app.webApiHandler.HandleFunc(router, "/realms/{realm}/protocol/openid-connect/token", app.webApiContext.IssueNewToken, http.MethodPost)
	// 3. Get userinfo endpoint - /auth/realms/SOAR/protocol/openid-connect/userinfo
	app.webApiHandler.HandleFunc(router, "/auth/realms/{realm}/protocol/openid-connect/userinfo", app.webApiContext.GetUserInfo, http.MethodGet)
	app.webApiHandler.HandleFunc(router, "/realms/{realm}/protocol/openid-connect/userinfo", app.webApiContext.GetUserInfo, http.MethodGet)
	// 4. OpenId Configuration endpoint
	app.webApiHandler.HandleFunc(router, "/auth/realms/{realm}/.well-known/openid-configuration", app.webApiContext.GetOpenIdConfiguration, http.MethodGet)
	app.webApiHandler.HandleFunc(router, "/realms/{realm}/.well-known/openid-configuration", app.webApiContext.GetOpenIdConfiguration, http.MethodGet)
}

func (app *Application) startWebService() error {
	var err error
	addressTemplate := "{0}:{1}"
	address := stringFormatter.Format(addressTemplate, app.appConfig.ServerCfg.Address, app.appConfig.ServerCfg.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	app.httpServer.Addr = address
	switch app.appConfig.ServerCfg.Schema { //nolint:exhaustive
	case config.HTTP:
		app.logger.Info(stringFormatter.Format("Starting \"HTTP\" WEB API Service on address: \"{0}\"", address))
		go func() {
			err = app.httpServer.Serve(listener)
			if err != nil {
				app.logger.Error(
					stringFormatter.Format("An error occurred during attempt to start \"HTTP\" WEB API Service: {0}", err.Error()))
			}
		}()
	case config.HTTPS:
		app.logger.Info(stringFormatter.Format("Starting \"HTTPS\" REST API Service on address: \"{0}\"", address))
		cert := app.appConfig.ServerCfg.Security.CertificateFile
		key := app.appConfig.ServerCfg.Security.KeyFile
		go func() {
			err = app.httpServer.ServeTLS(listener, cert, key)
			if err != nil {
				app.logger.Error(
					stringFormatter.Format("An error occurred during attempt tp start \"HTTPS\" REST API Service: {0}", err.Error()))
			}
		}()
	}
	return err
}

func (app *Application) readKey() []byte {
	absPath, err := filepath.Abs(*app.appConfigFile)
	if err != nil {
		app.logger.Error(stringFormatter.Format("An error occurred during getting key file abs path: {0}", err.Error()))
		return nil
	}

	fileData, err := os.ReadFile(absPath)
	if err != nil {
		app.logger.Error(stringFormatter.Format("An error occurred during key file reading: {0}", err.Error()))
		return nil
	}

	return fileData
}

func (app *Application) createHttpLoggingHandler(index int, router *mux.Router) *http.Handler {
	var resultRouter http.Handler = router

	destination := app.appConfig.Logging.Appenders[index].Destination
	lumberjackWriter := lumberjack.Logger{
		Filename:   string(destination.File),
		MaxSize:    destination.MaxSize,
		MaxAge:     destination.MaxAge,
		MaxBackups: destination.MaxBackups,
		LocalTime:  destination.LocalTime,
		Compress:   false,
	}

	if app.appConfig.Logging.LogHTTP {
		if app.appConfig.Logging.ConsoleOutHTTP {
			writer := io.MultiWriter(&lumberjackWriter, os.Stdout)
			resultRouter = handlers.LoggingHandler(writer, router)
		} else {
			resultRouter = handlers.LoggingHandler(&lumberjackWriter, router)
		}
	}
	return &resultRouter
}

func (app *Application) getSwaggerAddress() string {
	// 1. Get ENV Variable - FERRUM_SWAGGER_EXT_ADDRESS (see .env file)
	envAddr := os.Getenv(ferrumSwaggerAddressEnvVariable)
	if len(envAddr) > 0 {
		return envAddr
	}

	// 2. Get Address from Network Interfaces
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addresses {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
