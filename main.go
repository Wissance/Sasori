//go:generate go install github.com/swaggo/swag/cmd/swag@v1.7.6
//go:generate swag init --parseDependency --parseInternal --parseDepth 6 -o ./swagger
//go:generate openssl genrsa -out ./certs/server.key 2048
//go:generate openssl ecparam -genkey -name secp384r1 -out ./certs/server.key
//go:generate openssl req -new -x509 -sha256 -key ./certs/server.key -out ./certs/server.crt -days 3650 -subj "/C=RU"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wissance/Ferrum/application"
	"github.com/wissance/stringFormatter"
)

const defaultConfig = "./config.json"

var (
	configFile = flag.String("config", defaultConfig, "--config ./config_w_redis.json")
	devMode    = flag.Bool("devmode", false, "-devmode")
)

// main is an authorization server entry point is starts and stops by signal Application
/* Ferrum requires config to run via cmd line, if no config was provided defaultConfig is using
 * to start Ferrum with custom config (i.e. config_w_redis.json) execute following cmd ./ferrum --config ./config_w_redis.json
 * Ferrum stops by following signals:
 * 1. Interrupt = CTRL+C
 * 2. Terminate = signal from kill utility
 * 3. Hangup = also kill but with -9 arg - kill -9
 */
func main() {
	flag.Parse()
	osSignal := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(osSignal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	ctx := context.Background()

	app := application.CreateAppWithConfigs(*configFile, *devMode)
	_, initErr := app.Init()
	if initErr != nil {
		fmt.Printf("An error occurred during app init, terminating the app: %s\n", initErr)
		os.Exit(-1)
	}
	logger := app.GetLogger()
	logger.Info("Application was successfully initialized")

	res, err := app.Start()
	if !res {
		msg := stringFormatter.Format("An error occurred during starting application, error is: {0}", err.Error())
		fmt.Println(msg)
	} else {
		logger.Info("Application was successfully started")
	}

	// this goroutine handles OS signals and generate signal to stop the app
	go func() {
		sig := <-osSignal
		// logging.InfoLog(stringFormatter.Format("Got signal from OS: {0}", sig))
		logger.Info(stringFormatter.Format("Got signal from OS: \"{0}\", stopping", sig))
		done <- true
	}()
	// server was started in separate goroutine, main thread is waiting for signal to stop
	<-done

	res, err = app.Stop(ctx)
	if !res {
		msg := stringFormatter.Format("An error occurred during stopping application, error is: {0}", err.Error())
		fmt.Println(msg)
	} else {
		logger.Info("Application was successfully stopped")
	}
}
