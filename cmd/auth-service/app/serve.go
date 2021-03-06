package app

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/fezho/oidc-auth/cmd/auth-service/app/config"
	"github.com/fezho/oidc-auth/cmd/auth-service/app/options"
	"github.com/fezho/oidc-auth/server"
	"github.com/fezho/oidc-auth/version"
)

func CommandServe() *cobra.Command {
	opts, err := options.NewOptions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to initialize command options: %v", err)
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:  "auth-service",
		Long: `The auth-service is a oidc auth service`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runCommand(cmd, args, opts); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
		//Args: cobra.ExactArgs(0),
	}

	fs := cmd.Flags()
	opts.AddFlags(fs)

	return cmd
}

func runCommand(cmd *cobra.Command, args []string, opts *options.Options) error {
	if opts.PrintVersion {
		version.PrintVersionAndExit()
	}

	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "arguments %v are not supported for %q\n", args, cmd.CommandPath())
	}

	// load config, init log and validate config
	c, err := config.LoadConfigFromFile(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %v", opts.ConfigFile, err)
	}
	err = initLogger(c.Logger)
	if err != nil {
		return fmt.Errorf("invalid config: %v", err)
	}
	if err := c.Validate(); err != nil {
		return err
	}

	if c.Logger.Level != "" {
		log.Infof("use log level: %s", c.Logger.Level)
	}

	// initiate session storage
	if strings.HasPrefix(c.OIDC.RedirectURL, "https") {
		// set secureCookie if the scheme of the RedirectURL is https
		c.Storage.Config.SetSecureCookie(true)
	}
	storage, err := c.Storage.Config.Open()
	if err != nil {
		return fmt.Errorf("oidc-auth: open session storage: %v", err)
	}
	defer storage.Close()

	serverConfig := server.Config{
		DexAddress:     c.OIDC.DexAddress,
		IssuerURL:      c.OIDC.Issuer,
		RedirectURL:    c.OIDC.RedirectURL,
		ClientID:       c.OIDC.ClientID,
		ClientSecret:   c.OIDC.ClientSecret,
		Scopes:         c.OIDC.Scopes,
		UsernameClaim:  c.OIDC.UsernameClaim,
		GroupsClaim:    c.OIDC.GroupsClaim,
		Store:          storage,
		AllowedOrigins: c.Web.AllowedOrigins,
		OfflineAccess:  c.OIDC.OfflineAccess,
	}

	srv, err := server.NewServer(serverConfig)
	if err != nil {
		log.Fatal("failed to create auth server, ", err)
	}

	errc := make(chan error, 3)

	if c.Web.HTTP != "" {
		log.Infof("listening (http) on %s", c.Web.HTTP)
		go func() {
			err := http.ListenAndServe(c.Web.HTTP, srv)
			errc <- fmt.Errorf("listening on %s failed: %v", c.Web.HTTP, err)
		}()
	}

	if c.Web.HTTPS != "" {
		httpsSrv := &http.Server{
			Addr:    c.Web.HTTPS,
			Handler: srv,
			TLSConfig: &tls.Config{
				PreferServerCipherSuites: true,
				MinVersion:               tls.VersionTLS12,
			},
		}

		log.Infof("listening (https) on %s", c.Web.HTTPS)
		go func() {
			err = httpsSrv.ListenAndServeTLS(c.Web.TLSCert, c.Web.TLSKey)
			errc <- fmt.Errorf("listening on %s failed: %v", c.Web.HTTPS, err)
		}()
	}

	return <-errc
}

var logFormats = []string{"json", "text"}

type utcFormatter struct {
	f log.Formatter
}

func (f *utcFormatter) Format(e *log.Entry) ([]byte, error) {
	e.Time = e.Time.UTC()
	return f.f.Format(e)
}

func initLogger(config config.Logger) error {
	logLevel, err := log.ParseLevel(config.Level)
	if err != nil {
		return err
	}

	var formatter utcFormatter
	switch strings.ToLower(config.Format) {
	case "", "text":
		formatter.f = &log.TextFormatter{DisableColors: true}
	case "json":
		formatter.f = &log.JSONFormatter{}
	default:
		return fmt.Errorf("log format is not one of the supported values (%s): %s", strings.Join(logFormats, ", "), config.Format)
	}

	log.SetLevel(logLevel)
	log.SetFormatter(&formatter)

	return nil
}
