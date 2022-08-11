package main

import (
	"flag"
	"os"
	"os/signal"
	"time"

	"github.com/rs/zerolog"

	"github.com/openconfig/gnmi-gateway/gateway/benchmarks/statsd_server"
)

var (
	log = zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.InfoLevel)
)

type Config struct {
	Address string
	Port int
	ExpNotifications int
	StatsDumpIntervalS uint
	ResetStatsOnDump bool
}

func parseArgs(cfg *Config) {
	flag.StringVar(&cfg.Address, "address", "127.0.0.1", "The statsd server address")
	flag.IntVar(&cfg.Port, "port", 8125, "The statsd server port")
	flag.IntVar(
		&cfg.ExpNotifications, "expNotifications", 0,
		"The expected number of notifications, after which the server will close and " +
		"dump the stats")
	flag.UintVar(
		&cfg.StatsDumpIntervalS, "statsDumpInterval", 0,
		"Allows dumping the stats periodically, specified in seconds")
	flag.BoolVar(
		&cfg.ResetStatsOnDump, "resetStatsOnDump", false,
		"If enabled, the stats will be reset after being printed")

	flag.Parse()
}

func main() {
	cfg := &Config{}
	parseArgs(cfg)

	server, err := statsd_server.NewServer(
		cfg.Address, cfg.Port, cfg.ExpNotifications, log)
	if err != nil {
		panic(err)
	}

	stopSignal := make(chan os.Signal, 1)
	signal.Notify(stopSignal, os.Interrupt)

	go func() {
		<- stopSignal
		log.Info().Msg("received stop signal, cleaning up")
		server.Stop()
	}()

	if cfg.StatsDumpIntervalS > 0 {
		go func() {
			for {
				time.Sleep(time.Duration(cfg.StatsDumpIntervalS) * time.Second)
				server.PrintStats()

				if cfg.ResetStatsOnDump {
					server.ResetStats()
				}
			}
		}()
	}

	if err := server.Start(); err != nil {
		panic(err)
	}

	server.Wait()
	server.PrintStats()
}
