package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/balancers"
)

var (
	dsn              string
	prefix           string
	port             int
	sessionPoolLimit int
	shutdownAfter    time.Duration
	logLevel         string

	log = zerolog.New(os.Stdout).With().Timestamp().Logger()
)

func init() {
	required := []string{"ydb"}
	flagSet := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flagSet.Usage = func() {
		out := flagSet.Output()
		_, _ = fmt.Fprintf(out, "Usage:\n%s [options]\n", os.Args[0])
		_, _ = fmt.Fprintf(out, "\nOptions:\n")
		flagSet.PrintDefaults()
	}
	flagSet.StringVar(&dsn,
		"ydb", "",
		"YDB connection string",
	)
	flagSet.StringVar(&prefix,
		"prefix", "",
		"tables prefix",
	)
	flagSet.StringVar(&logLevel,
		"log-level", "info",
		"logging level",
	)
	flagSet.IntVar(&port,
		"port", 80,
		"http port for web-server",
	)
	flagSet.IntVar(&sessionPoolLimit,
		"session-pool-limit", 50,
		"session pool size limit",
	)
	flagSet.DurationVar(&shutdownAfter,
		"shutdown-after", -1,
		"duration for shutdown after start",
	)
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		flagSet.Usage()
		os.Exit(1)
	}
	flagSet.Visit(func(f *flag.Flag) {
		for i, arg := range required {
			if arg == f.Name {
				required = append(required[:i], required[i+1:]...)
			}
		}
	})
	if len(required) > 0 {
		fmt.Printf("\nSome required options not defined: %v\n\n", required)
		flagSet.Usage()
		os.Exit(1)
	}
	if l, err := zerolog.ParseLevel(logLevel); err == nil {
		zerolog.SetGlobalLevel(l)
	} else {
		panic(err)
	}
}

func main() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		done   = make(chan struct{})
	)
	if shutdownAfter > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), shutdownAfter)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	s, err := newService(
		ctx,
		dsn,
		ydb.WithSessionPoolSizeLimit(sessionPoolLimit),
		ydb.WithSessionPoolKeepAliveTimeout(5*time.Second),
		ydb.WithBalancer(
			balancers.Prefer(
				balancers.RandomChoice(),
				func(endpoint balancers.Endpoint) bool {
					return endpoint.Address() == "kikimr0425.search.yandex.net:31051" || endpoint.Address() == "kikimr0447.search.yandex.net:31051"
				},
			),
		),
	)
	if err != nil {
		fmt.Println()
		fmt.Println("Create service failed. Re-run with flag '-log-level=warn' and see logs")
		os.Exit(1)
	}
	defer s.Close(ctx)

	server := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: s.router,
	}
	defer func() {
		_ = server.Shutdown(ctx)
	}()

	go func() {
		_ = server.ListenAndServe()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return
	case <-done:
		return
	}
}
