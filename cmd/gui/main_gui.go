package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/allan-simon/go-singleinstance"

	"github.com/rupor-github/gclpr/misc"
	"github.com/rupor-github/gclpr/server"
	"github.com/rupor-github/gclpr/systray"
	"github.com/rupor-github/gclpr/util"
)

var (
	aPort       int
	aLE         string
	aHelp       bool
	aUnlocked   bool
	aDebug      bool
	aIOTimeout  time.Duration
	usageString string
	lock        int32
	clipCancel  context.CancelFunc
	clipCtx     context.Context
	title       = "gclpr-gui"
	tooltip     = "Notification tray wrapper for gclpr"
	cli         = flag.NewFlagSet(title, flag.ContinueOnError)
)

func onReady() {

	log.Print("Entering systray")

	systray.SetIcon(systray.MakeIntResource(1000))
	systray.SetTitle(title)
	systray.SetTooltip(tooltip)

	miHelp := systray.AddMenuItem("About", "Shows application help")
	systray.AddSeparator()
	miQuit := systray.AddMenuItem("Exit", "Exits application")

	go func() {
		for {
			select {
			case <-miHelp.ClickedCh:
				util.ShowOKMessage(util.MsgInformation, title, usageString)
			case <-miQuit.ClickedCh:
				log.Print("Requesting exit")
				systray.Quit()
				return
			}
		}
	}()
}

func onSession(e systray.SessionEvent) {
	switch e {
	case systray.SesLock:
		atomic.StoreInt32(&lock, 1)
		log.Print("Session locked")
	case systray.SesUnlock:
		atomic.StoreInt32(&lock, 0)
		log.Print("Session unlocked")
	default:
	}
}

func onExit() {
	// stop servicing clipboard and uri requests
	clipCancel()
	log.Print("Exiting systray")
}

func clipStart() error {

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("unable to get user directory: %w", err)
	}

	pkeys, err := util.ReadTrustedKeys(home)
	if err != nil {
		return err
	}

	log.Printf("Starting server with %d trusted public key(s)\n", len(pkeys))
	if len(pkeys) == 0 {
		return fmt.Errorf("no keys to serve")
	}

	clipCtx, clipCancel = context.WithCancel(context.Background())
	go func() {
		locked := &lock
		if aUnlocked {
			locked = nil // ignore session messages
		}
		if err := server.Serve(clipCtx, aPort, aLE, pkeys, misc.GetMagic(), locked, aIOTimeout); err != nil {
			log.Printf("gclpr serve() returned error: %s", err.Error())
		}
	}()
	return nil
}

func buildUsageString() string {
	var buf = new(strings.Builder)
	cli.SetOutput(buf)
	fmt.Fprintf(buf, `
%s

Version:
    %s (%s) %s
`, tooltip, misc.GetVersion(), runtime.Version(), misc.GetGitHash())

	fmt.Fprintf(buf, `
Usage:
    %s [options]...

Options:

`, title)
	cli.PrintDefaults()
	return buf.String()
}

func main() {

	util.NewLogWriter(title, 0, false)

	cli.BoolVar(&aHelp, "h", false, "Show help")
	cli.BoolVar(&aHelp, "help", false, "Show help")
	cli.IntVar(&aPort, "port", server.DefaultPort, "TCP port number")
	cli.StringVar(&aLE, "line-ending", "", "Convert Line Endings (LF/CRLF)")
	cli.DurationVar(&aIOTimeout, "timeout", server.DefaultIOTimeout, "Read/write I/O timeout")
	cli.BoolVar(&aUnlocked, "ignore-session-lock", false, "Continue to access clipboard inside locked session")
	cli.BoolVar(&aDebug, "debug", false, "Print debugging information")

	if err := cli.Parse(os.Args[1:]); err != nil {
		util.ShowOKMessage(util.MsgError, title, err.Error())
		os.Exit(1)
	}

	usageString = buildUsageString()

	if aHelp {
		util.ShowOKMessage(util.MsgInformation, title, usageString)
		os.Exit(0)
	}

	util.NewLogWriter(title, 0, aDebug)

	// Only allow single instance of gui to run
	lockName := filepath.Join(os.TempDir(), title+".lock")
	inst, err := singleinstance.CreateLockFile(lockName)
	if err != nil {
		log.Print("Application already running")
		os.Exit(1)
	}

	if err := clipStart(); err != nil {
		util.ShowOKMessage(util.MsgInformation, title, err.Error())
		os.Exit(1)
	}

	systray.Run(onReady, onExit, onSession)

	// Not necessary
	inst.Close()
	os.Remove(lockName)
}
