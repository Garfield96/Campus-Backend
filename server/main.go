package main

import (
	"github.com/TUM-Dev/Campus-Backend/backend"
	"github.com/TUM-Dev/Campus-Backend/backend/cron"
	"github.com/TUM-Dev/Campus-Backend/backend/migration"
	"github.com/TUM-Dev/Campus-Backend/web"
	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net"
	"os"
)

const (
	httpPort = ":50051"
	grpcPort = ":50052"
)

var Version = "dev"

func main() {
	// Connect to DB
	var conn gorm.Dialector
	shouldAutoMigrate := false
	if dbHost := os.Getenv("DB_DSN"); dbHost != "" {
		log.Info("Connecting to dsn")
		conn = mysql.Open(dbHost)
	} else {
		conn = sqlite.Open("test.db")
		shouldAutoMigrate = true
	}

	environment := "development"
	if Version != "dev" {
		environment = "production"
	}
	if sentryDSN := os.Getenv("SENTRY_DSN"); sentryDSN != "" {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:         os.Getenv("SENTRY_DSN"),
			Release:     Version,
			Environment: environment,
		}); err != nil {
			log.WithError(err).Error("Sentry initialization failed")
		}
	} else {
		log.Println("continuing without sentry")
	}
	db, err := gorm.Open(conn, &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schema
	tumMigrator := migration.New(db, shouldAutoMigrate)
	err = tumMigrator.Migrate()
	if err != nil {
		log.WithError(err).Fatal("Failed to migrate database")
		return
	}

	// Create any other background services (these shouldn't do any long running work here)
	cronService := cron.New(db)
	campusService := backend.New(db)

	// Listen to our configured ports
	grpcListener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	httpListener, err := net.Listen("tcp", httpPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	g := errgroup.Group{}
	// Start each server in its own go routine and logs any errors
	g.Go(func() error { return campusService.GRPCServe(grpcListener) })
	g.Go(func() error { return web.HTTPServe(httpListener, grpcPort) })

	// Setup cron jobs
	g.Go(func() error { return cronService.Run() })

	log.Println("run server: ", g.Wait())
}
