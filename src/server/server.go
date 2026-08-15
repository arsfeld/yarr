package server

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nkanaev/yarr/src/storage"
	"github.com/nkanaev/yarr/src/worker"
)

type Server struct {
	Addr   string
	db     storage.Storage
	worker *worker.Worker

	BasePath string

	// auth
	Username string
	Password string
	// https
	CertFile string
	KeyFile  string

	// google reader api action tokens, keyed by token with their expiry time
	greaderTokens      map[string]time.Time
	greaderTokensMutex sync.Mutex
}

func NewServer(db storage.Storage, addr string) *Server {
	return &Server{
		db:            db,
		Addr:          addr,
		worker:        worker.NewWorker(db),
		greaderTokens: make(map[string]time.Time),
	}
}

func (h *Server) GetAddr() string {
	proto := "http"
	if h.CertFile != "" && h.KeyFile != "" {
		proto = "https"
	}
	return proto + "://" + h.Addr + h.BasePath
}

func (s *Server) Start() {
	refreshRate := s.db.GetSettings().RefreshRate
	s.worker.StartFeedCleaner()
	s.worker.SetRefreshRate(refreshRate)

	var ln net.Listener
	var err error

	if path, isUnix := strings.CutPrefix(s.Addr, "unix:"); isUnix {
		err = os.Remove(path)
		if err != nil {
			log.Print(err)
		}
		ln, err = net.Listen("unix", path)
	} else {
		ln, err = net.Listen("tcp", s.Addr)
	}

	if err != nil {
		log.Fatal(err)
	}

	httpserver := &http.Server{Handler: s.handler()}
	if s.CertFile != "" && s.KeyFile != "" {
		err = httpserver.ServeTLS(ln, s.CertFile, s.KeyFile)
		ln.Close()
	} else {
		err = httpserver.Serve(ln)
	}

	if err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
