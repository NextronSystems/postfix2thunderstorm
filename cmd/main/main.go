package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"postfix2thunderstorm/service"
	"sync"
	"syscall"

	"github.com/antonmedv/expr"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

var logger = func() *logrus.Entry {
	l := logrus.New()
	return logrus.NewEntry(l)
}()

var DebugFlag bool
var ConfigFlag string

func main() {
	flag.BoolVar(&DebugFlag, "debug", false, "Debug flag")
	flag.StringVar(&ConfigFlag, "config", "./p2t.config.yaml", "Config filepath")
	flag.Parse()

	raw, err := os.ReadFile(ConfigFlag)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	config := service.Config{}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		log.Fatal("Failed to parse config file", err)
	}

	l := logrus.New()
	if DebugFlag {
		l.SetOutput(os.Stdout)
		l.SetLevel(logrus.DebugLevel)
	} else {
		l.SetFormatter(&logrus.JSONFormatter{})
		l.SetLevel(logrus.InfoLevel)
		l.SetOutput(&lumberjack.Logger{
			Filename:   config.LogFilePath,
			MaxSize:    500, // megabytes
			MaxBackups: 3,
			MaxAge:     31, //days
		})
	}
	logger = l.WithField("component", "postfix2thor")

	logger.Infof("using config: %+v", config)

	if vm, err := expr.Compile(config.Expression, expr.Env(service.Env{})); err != nil {
		logger.Fatalf("expression error: %v", err)
	} else {
		returned, err := expr.Run(vm, service.Env{FullMatch: service.ThorThunderStormMatch{}, Matches: []service.ThorThunderStormMatchItem{}})
		if err != nil {
			logger.Fatalf("expression error: %v", err)
		}
		_, ok := returned.(bool)
		if !ok {
			logger.Fatal("expression error: not a bool expression")
		}
	}

	milter := service.NewMilterService(config, logger)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := milter.Run(); err != nil {
			logger.Fatal(err)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	milter.Stop()
	wg.Wait()
}
