package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kokizzu/external-dns-gcore-webhook/gcoreprovider"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

// https://patorjk.com/software/taag/
const banner = `
  _________________                       
 /  _____/\_   ___ \  ___________   ____  
/   \  ___/    \  \/ /  _ \_  __ \_/ __ \ 
\    \_\  \     \___(  <_> )  | \/\  ___/ 
 \______  /\______  /\____/|__|    \___  >
        \/        \/                   \/ 

 external-dns-gcore
 version: %s

`

var (
	Version    = "v0.0.1"
	ApiKey     = ``
	ServerHost = ``
	ServerPort = `8080`
	DryRun     = false
)

func main() {
	log.SetLevel(log.DebugLevel)
	fmt.Printf(banner, Version)
	ApiKey = os.Getenv(gcoreprovider.EnvAPIToken)
	ServerHost = os.Getenv(`SERVER_HOST`)
	ServerPort = os.Getenv(`SERVER_PORT`)
	if ServerPort == `` {
		ServerPort = `8888`
	}
	DryRun = os.Getenv(`DRY_RUN`) == `true`

	provider, err := gcoreprovider.NewProvider(endpoint.DomainFilter{}, ApiKey, DryRun)
	if err != nil {
		log.Fatalf("Failed to initialize DNS provider: %v", err)
	}
	server := CreateWebServer(provider)
	server.Start()
}

type webServer struct {
	*http.Server
}

func (w *webServer) Start() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	sig := <-sigCh
	log.Printf("shutting down server due to received signal: %v", sig)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := w.Shutdown(ctx); err != nil {
		log.Printf("error shutting down server: %v", err)
	}
	cancel()
}

// CreateWebServer will respond to the following endpoints:
// - / (GET): initialization, negotiates headers and returns the domain filter
// - /records (GET): returns the current records
// - /records (POST): applies the changes
// - /adjustendpoints (POST): executes the AdjustEndpoints method
func CreateWebServer(p *gcoreprovider.DnsProvider) *webServer {

	r := chi.NewRouter()
	r.Get(`/health`, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { // negotiate
		if err := acceptHeaderCheck(w, r); err != nil {
			requestLog(r).WithField(logFieldError, err).Error("accept header check failed")
			return
		}
		b, err := p.GetDomainFilter().MarshalJSON()
		if err != nil {
			log.Errorf("failed to marshal domain filter, request method: %s, request path: %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set(contentTypeHeader, string(mediaTypeVersion1))
		if _, writeError := w.Write(b); writeError != nil {
			requestLog(r).WithField(logFieldError, writeError).Error("error writing response")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	})
	r.Get("/records", func(w http.ResponseWriter, r *http.Request) {
		if err := acceptHeaderCheck(w, r); err != nil {
			requestLog(r).WithField(logFieldError, err).Error("accept header check failed")
			return
		}
		requestLog(r).Debug("requesting records")
		ctx := r.Context()
		records, err := p.Records(ctx)
		if err != nil {
			requestLog(r).WithField(logFieldError, err).Error("error getting records")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requestLog(r).Debugf("returning records count: %d", len(records))
		w.Header().Set(contentTypeHeader, string(mediaTypeVersion1))
		w.Header().Set(varyHeader, contentTypeHeader)
		err = json.NewEncoder(w).Encode(records)
		if err != nil {
			requestLog(r).WithField(logFieldError, err).Error("error encoding records")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	})
	r.Post("/records", func(w http.ResponseWriter, r *http.Request) {
		if err := contentTypeHeaderCheck(w, r); err != nil {
			requestLog(r).WithField(logFieldError, err).Error("content type header check failed")
			return
		}
		var changes plan.Changes
		ctx := r.Context()
		if err := json.NewDecoder(r.Body).Decode(&changes); err != nil {
			w.Header().Set(contentTypeHeader, contentTypePlaintext)
			w.WriteHeader(http.StatusBadRequest)
			errMsg := fmt.Sprintf("error decoding changes: %s", err.Error())
			if _, writeError := fmt.Fprint(w, errMsg); writeError != nil {
				requestLog(r).WithField(logFieldError, writeError).Fatalf("error writing error message to response writer")
			}
			requestLog(r).WithField(logFieldError, err).Info(errMsg)
			return
		}
		requestLog(r).Debugf("requesting apply changes, create: %d , updateOld: %d, updateNew: %d, delete: %d",
			len(changes.Create), len(changes.UpdateOld), len(changes.UpdateNew), len(changes.Delete))
		if err := p.ApplyChanges(ctx, &changes); err != nil {
			w.Header().Set(contentTypeHeader, contentTypePlaintext)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/adjustendpoints", func(w http.ResponseWriter, r *http.Request) {
		if err := contentTypeHeaderCheck(w, r); err != nil {
			log.Errorf("content type header check failed, request method: %s, request path: %s", r.Method, r.URL.Path)
			return
		}
		if err := acceptHeaderCheck(w, r); err != nil {
			log.Errorf("accept header check failed, request method: %s, request path: %s", r.Method, r.URL.Path)
			return
		}

		var pve []*endpoint.Endpoint
		if err := json.NewDecoder(r.Body).Decode(&pve); err != nil {
			w.Header().Set(contentTypeHeader, contentTypePlaintext)
			w.WriteHeader(http.StatusBadRequest)
			errMessage := fmt.Sprintf("failed to decode request body: %v", err)
			log.Infof(errMessage+" , request method: %s, request path: %s", r.Method, r.URL.Path)
			if _, writeError := fmt.Fprint(w, errMessage); writeError != nil {
				requestLog(r).WithField(logFieldError, writeError).Fatalf("error writing error message to response writer")
			}
			return
		}
		log.Debugf("requesting adjust endpoints count: %d", len(pve))
		pve, _ = p.AdjustEndpoints(pve)
		out, _ := json.Marshal(&pve)
		log.Debugf("return adjust endpoints response, resultEndpointCount: %d", len(pve))
		w.Header().Set(contentTypeHeader, string(mediaTypeVersion1))
		w.Header().Set(varyHeader, contentTypeHeader)
		if _, writeError := fmt.Fprint(w, string(out)); writeError != nil {
			requestLog(r).WithField(logFieldError, writeError).Fatalf("error writing response")
		}
	})

	srv := &webServer{
		Server: &http.Server{
			Addr:    fmt.Sprintf("%s:%s", ServerHost, ServerPort),
			Handler: r,
		}}
	go func() {
		log.Printf("starting server on addr: '%s' ", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("can't serve on addr: '%s', error: %v", srv.Addr, err)
		}
	}()
	return srv
}

const (
	mediaTypeFormat        = "application/external.dns.webhook+json;"
	contentTypeHeader      = "Content-Type"
	contentTypePlaintext   = "text/plain"
	acceptHeader           = "Accept"
	varyHeader             = "Vary"
	supportedMediaVersions = "1"
	logFieldRequestPath    = "requestPath"
	logFieldRequestMethod  = "requestMethod"
	logFieldError          = "error"
)

func contentTypeHeaderCheck(w http.ResponseWriter, r *http.Request) error {
	return headerCheck(true, w, r)
}

func acceptHeaderCheck(w http.ResponseWriter, r *http.Request) error {
	return headerCheck(false, w, r)
}

func headerCheck(isContentType bool, w http.ResponseWriter, r *http.Request) error {
	var header string
	if isContentType {
		header = r.Header.Get(contentTypeHeader)
	} else {
		header = r.Header.Get(acceptHeader)
	}
	if len(header) == 0 {
		w.Header().Set(contentTypeHeader, contentTypePlaintext)
		w.WriteHeader(http.StatusNotAcceptable)
		msg := "client must provide "
		if isContentType {
			msg += "a content type"
		} else {
			msg += "an accept header"
		}
		err := fmt.Errorf(msg)
		_, writeErr := fmt.Fprint(w, err.Error())
		if writeErr != nil {
			requestLog(r).WithField(logFieldError, writeErr).Fatalf("error writing error message to response writer")
		}
		return err
	}
	// as we support only one media type version, we can ignore the returned value
	if _, err := checkAndGetMediaTypeHeaderValue(header); err != nil {
		w.Header().Set(contentTypeHeader, contentTypePlaintext)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		msg := "client must provide a valid versioned media type in the "
		if isContentType {
			msg += "content type"
		} else {
			msg += "accept header"
		}
		err := fmt.Errorf(msg+": %s", err.Error())
		_, writeErr := fmt.Fprint(w, err.Error())
		if writeErr != nil {
			requestLog(r).WithField(logFieldError, writeErr).Fatalf("error writing error message to response writer")
		}
		return err
	}
	return nil
}

func requestLog(r *http.Request) *log.Entry {
	return log.WithFields(log.Fields{logFieldRequestMethod: r.Method, logFieldRequestPath: r.URL.Path})
}

var mediaTypeVersion1 = mediaTypeVersion("1")

type mediaType string

func mediaTypeVersion(v string) mediaType {
	return mediaType(mediaTypeFormat + "version=" + v)
}

func (m mediaType) Is(headerValue string) bool {
	return string(m) == headerValue
}

func checkAndGetMediaTypeHeaderValue(value string) (string, error) {
	for _, v := range strings.Split(supportedMediaVersions, ",") {
		if mediaTypeVersion(v).Is(value) {
			return v, nil
		}
	}
	supportedMediaTypesString := ""
	for i, v := range strings.Split(supportedMediaVersions, ",") {
		sep := ""
		if i < len(supportedMediaVersions)-1 {
			sep = ", "
		}
		supportedMediaTypesString += string(mediaTypeVersion(v)) + sep
	}
	return "", fmt.Errorf("unsupported media type version: '%s'. Supported media types are: '%s'", value, supportedMediaTypesString)
}
