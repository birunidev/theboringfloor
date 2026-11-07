package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/runstate"
)

func listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, fmt.Errorf("officeview only permits 127.0.0.1:<port>")
	}
	return net.Listen("tcp4", address)
}

func main() {
	bind := flag.String("bind", "127.0.0.1:0", "IPv4 loopback address")
	home := flag.String("home", runstate.DefaultRoot(), "autopilot home")
	controls := flag.Bool("controls", false, "enable authenticated STOP controls")
	testData := flag.Bool("test-data", false, "visibly label externally supplied test telemetry")
	flag.Parse()
	ln, err := listen(*bind)
	if err != nil {
		log.Fatal(err)
	}
	view, err := newOffice(*home, "http://"+ln.Addr().String(), *controls, *testData)
	if err != nil {
		ln.Close()
		log.Fatal(err)
	}
	srv := &http.Server{Handler: view, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	fmt.Println(view.origin)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
