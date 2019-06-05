package render

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/strongdm/comply/internal/config"
	"github.com/yosssi/ace"
)

var ServePort int

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var aceOpts = &ace.Options{
	DynamicReload: true,
	Indent:        "  ",
}

var watchChMu sync.Mutex
var watchCh chan struct{}

func subscribe() chan struct{} {
	watchChMu.Lock()
	defer watchChMu.Unlock()
	if watchCh == nil {
		watchCh = make(chan struct{})
	}
	return watchCh
}

func broadcast() {
	watchChMu.Lock()
	defer watchChMu.Unlock()
	close(watchCh)
	watchCh = nil
}

var lastModifiedMu sync.Mutex
var lastModified = make(map[string]time.Time)

func recordModified(path string, t time.Time) {
	lastModifiedMu.Lock()
	defer lastModifiedMu.Unlock()

	previous, ok := lastModified[path]
	if !ok || t.After(previous) {
		lastModified[path] = t
	}
}

func isNewer(path string, t time.Time) bool {
	lastModifiedMu.Lock()
	defer lastModifiedMu.Unlock()

	previous, ok := lastModified[path]
	if !ok {
		return true
	}

	// is tested after previous? Then isNewer is true.
	return t.After(previous)
}

func listFolders(root string) ([]string, error) {
	var folders []string
	dirInfo, err := ioutil.ReadDir(root)
	if err != nil {
		return folders, err
	}

	for _, entry := range dirInfo {
		if entry.IsDir() {
			folders = append(folders, entry.Name())
		}
	}
	return folders, nil
}

func serveStaticFolder(folderName string) {
	http.Handle("/"+folderName+"/", http.FileServer(http.Dir("./static")))
}

// Build generates all PDF and HTML output to the target directory with optional live reload.
func Build(output string, live bool) error {
	err := os.RemoveAll(output)
	if err != nil {
		errors.Wrap(err, "unable to remove files from output directory")
	}

	err = os.MkdirAll(output+"/"+config.Config().PDFFolder, os.FileMode(0755))
	if err != nil {
		errors.Wrap(err, "unable to create output directory")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 0)
	wgCh := make(chan struct{})

	if live {
		watch(errCh)

		staticFolders, err := listFolders("./static")
		if err != nil {
			errors.Wrap(err, "unable to list subfolders in static")
		}

		go func() {
			for _, folderName := range staticFolders {
				serveStaticFolder(folderName)
			}
			http.Handle("/", http.FileServer(http.Dir("./output")))
			err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", ServePort), nil)
			if err != nil {
				panic(err)
			}
		}()

		fmt.Printf("Serving content of output/ at http://127.0.0.1:%d (ctrl-c to quit)\n", ServePort)
	}
	// PDF
	wg.Add(1)
	go pdf(output, live, errCh, &wg)

	// HTML
	wg.Add(1)
	go html(output, live, errCh, &wg)

	// WG monitor
	go func() {
		wg.Wait()
		close(wgCh)
	}()

	select {
	case <-wgCh:
		// success
	case err := <-errCh:
		return errors.Wrap(err, "error during build")
	}

	return nil
}
