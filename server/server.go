package server

import (
	"context"
	"github.com/coreos/go-oidc"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"net/http"
)

type Config struct {
	// URL of the OpenID Connect issuer
	IssuerURL string
	// callback url for OpenID Connect Provider response.
	RedirectURL string
	// OAuth2 client ID of this application
	ClientID string
	// OAuth2 client secret of this application
	ClientSecret string
	// Scope specifies optional requested permissions
	Scopes []string
	// UsernameClaim is the JWT field to use as the user's username.
	UsernameClaim string
	// GroupsClaim, if specified, causes the OIDCAuthenticator to try to populate the user's
	// groups with an ID Token field. If the GroupsClaim field is present in an ID Token the value
	// must be a string or list of strings.
	GroupsClaim string
	// backend session store
	Store sessions.Store
	// CORS allowed origins
	AllowedOrigins []string
	// Whether to use AccessTypeOffline or not
	OfflineAccess bool
}

type Server struct {
	provider     *oidc.Provider
	store        sessions.Store
	oauth2Config *oauth2.Config

	usernameClaim string
	groupsClaim   string
	authCodeOpts  []oauth2.AuthCodeOption

	mux http.Handler
}

type UserIDOpts struct {
	Header      string
	TokenHeader string
	Prefix      string
	Claim       string
}

func NewServer(config Config) (*Server, error) {
	provider, err := oidc.NewProvider(context.Background(), config.IssuerURL)
	if err != nil {
		return nil, errors.Errorf("failed to get provider %q: %v", config.IssuerURL, err)
	}

	// This is the only mandatory scope and will return a sub claim
	// which represents a unique identifier for the authenticated user.
	oidcScopes := append(config.Scopes, oidc.ScopeOpenID)

	s := &Server{
		provider: provider,
		oauth2Config: &oauth2.Config{
			RedirectURL:  config.RedirectURL,
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     provider.Endpoint(),
			Scopes:       oidcScopes,
		},
		store:         config.Store,
		usernameClaim: config.UsernameClaim,
		groupsClaim:   config.GroupsClaim,
	}

	if config.OfflineAccess {
		s.authCodeOpts = append(s.authCodeOpts, oauth2.AccessTypeOffline)
	}

	router := mux.NewRouter()
	// Authorization redirect callback from OAuth2 auth flow.
	router.HandleFunc("/callback", s.callback).Methods(http.MethodGet)
	//router.HandleFunc("/logout", bearerTokenHandler(s.logout)).Methods(http.MethodGet)
	router.HandleFunc("/logout", s.logout).Methods(http.MethodGet)
	router.HandleFunc("/refresh_token", bearerTokenHandler(s.refreshToken)).Methods(http.MethodGet)
	//router.HandleFunc("/", bearerTokenHandler(s.auth))
	router.HandleFunc("/", s.auth)
	router.Handle("/healthz", s.healthCheck(context.Background()))

	// TODO: add session detail?

	s.mux = router
	if len(config.AllowedOrigins) > 0 {
		corsOption := handlers.AllowedOrigins(config.AllowedOrigins)
		s.mux = handlers.CORS(corsOption)(router)
	}

	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
