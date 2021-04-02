package interrupt

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	
	uberatomic "go.uber.org/atomic"
	
	"github.com/p9c/pod/pkg/util/qu"
	
	"github.com/kardianos/osext"
)

type HandlerWithSource struct {
	Source string
	Fn     func()
}

var (
	Restart bool // = true
	requested uberatomic.Bool
	// ch is used to receive SIGINT (Ctrl+C) signals.
	ch chan os.Signal
	// signals is the list of signals that cause the interrupt
	signals = []os.Signal{os.Interrupt}
	// ShutdownRequestChan is a channel that can receive shutdown requests
	ShutdownRequestChan = qu.T()
	// addHandlerChan is used to add an interrupt handler to the list of
	// handlers to be invoked on SIGINT (Ctrl+C) signals.
	addHandlerChan = make(chan HandlerWithSource)
	// HandlersDone is closed after all interrupt handlers run the first time
	// an interrupt is signaled.
	HandlersDone = make(qu.C)
)

var interruptCallbacks []func()
var interruptCallbackSources []string

// Listener listens for interrupt signals, registers interrupt callbacks,
// and responds to custom shutdown signals as required
func Listener() {
	invokeCallbacks := func() {
		D.Ln(
			"running interrupt callbacks",
			len(interruptCallbacks),
			strings.Repeat(" ", 48),
			interruptCallbackSources,
		)
		// run handlers in LIFO order.
		for i := range interruptCallbacks {
			idx := len(interruptCallbacks) - 1 - i
			D.Ln("running callback", idx, interruptCallbackSources[idx])
			interruptCallbacks[idx]()
		}
		D.Ln("interrupt handlers finished")
		HandlersDone.Q()
		if Restart {
			var file string
			var e error
			file, e = osext.Executable()
			if e != nil {
				E.Ln(e)
				return
			}
			D.Ln("restarting")
			if runtime.GOOS != "windows" {
				e = syscall.Exec(file, os.Args, os.Environ())
				if e != nil {
					F.Ln(e)
				}
			} else {
				D.Ln("doing windows restart")
				
				// procAttr := new(os.ProcAttr)
				// procAttr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
				// os.StartProcess(os.Args[0], os.Args[1:], procAttr)
				
				var s []string
				// s = []string{"cmd.exe", "/C", "start"}
				s = append(s, os.Args[0])
				// s = append(s, "--delaystart")
				s = append(s, os.Args[1:]...)
				cmd := exec.Command(s[0], s[1:]...)
				D.Ln("windows restart done")
				if e = cmd.Start(); E.Chk(e) {
				}
				// // select{}
				// os.Exit(0)
			}
		}
		// time.Sleep(time.Second * 3)
		// os.Exit(1)
		// close(HandlersDone)
	}
out:
	for {
		select {
		case sig := <-ch:
			// if !requested {
			// 	L.Printf("\r>>> received signal (%s)\n", sig)
			D.Ln("received interrupt signal", sig)
			requested.Store(true)
			invokeCallbacks()
			// pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
			// }
			break out
		case <-ShutdownRequestChan.Wait():
			// if !requested {
			W.Ln("received shutdown request - shutting down...")
			requested.Store(true)
			invokeCallbacks()
			break out
			// }
		case handler := <-addHandlerChan:
			// if !requested {
			// D.Ln("adding handler")
			interruptCallbacks = append(interruptCallbacks, handler.Fn)
			interruptCallbackSources = append(interruptCallbackSources, handler.Source)
			// }
		case <-HandlersDone.Wait():
			break out
		}
	}
}

// AddHandler adds a handler to call when a SIGINT (Ctrl+C) is received.
func AddHandler(handler func()) {
	// Create the channel and start the main interrupt handler which invokes all other callbacks and exits if not
	// already done.
	_, loc, line, _ := runtime.Caller(1)
	msg := fmt.Sprintf("%s:%d", loc, line)
	D.Ln("handler added by:", msg)
	if ch == nil {
		ch = make(chan os.Signal)
		signal.Notify(ch, signals...)
		go Listener()
	}
	addHandlerChan <- HandlerWithSource{
		msg, handler,
	}
}

// Request programmatically requests a shutdown
func Request() {
	_, f, l, _ := runtime.Caller(1)
	D.Ln("interrupt requested", f, l, requested.Load())
	if requested.Load() {
		D.Ln("requested again")
		return
	}
	requested.Store(true)
	ShutdownRequestChan.Q()
	// qu.PrintChanState()
	var ok bool
	select {
	case _, ok = <-ShutdownRequestChan:
	default:
	}
	D.Ln("shutdownrequestchan", ok)
	if ok {
		close(ShutdownRequestChan)
	}
}

// GoroutineDump returns a string with the current goroutine dump in order to show what's going on in case of timeout.
func GoroutineDump() string {
	buf := make([]byte, 1<<18)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// RequestRestart sets the reset flag and requests a restart
func RequestRestart() {
	Restart = true
	D.Ln("requesting restart")
	Request()
}

// Requested returns true if an interrupt has been requested
func Requested() bool {
	return requested.Load()
}
