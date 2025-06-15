package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/big" // Used for cryptographically secure random port number
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/jackpal/gateway"
	"github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

var (
	inputAddress string
	inputPort    string

	openCmd = &cobra.Command{
		Use:   "open [path]",
		Short: "Open file or directory in a browser with a beautiful, secure server UI",
		Long: `Serves a file or directory with a modern web interface protected by a unique access token.
A new token is generated each time the server starts. The URL with the token is printed and available via QR code.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("requires a file or directory path")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			urlBase := fmt.Sprintf("%s:%s", inputAddress, inputPort)
			fileDir, fileBase := parsePath(args[0])
			token, err := generateRandomToken(16)
			if err != nil {
				log.Fatalf("FATAL: Could not generate security token: %v", err)
			}
			qrCode(urlBase, fileBase, token)
			serveFiles(urlBase, fileDir, token)
		},
	}
)

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>File Server</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; margin: 0; background-color: #f8f9fa; color: #343a40; }
        .container { max-width: 800px; margin: 40px auto; padding: 0 20px; }
        h2 { color: #007bff; border-bottom: 2px solid #dee2e6; padding-bottom: 10px; margin-top: 40px;}
        .upload-box { border: 3px dashed #007bff; border-radius: 10px; padding: 40px; text-align: center; margin-bottom: 30px; background-color: #fff; transition: background-color 0.2s ease-in-out; cursor: pointer; }
        .upload-box p { margin: 0 0 15px 0; font-size: 1.2em; }
        .upload-box:hover, .upload-box.dragover { background-color: #e9ecef; }
        #file-input { display: none; }
        .file-list { list-style: none; padding: 0; border: 1px solid #dee2e6; border-radius: 5px; background-color: #fff; }
        .file-list li { padding: 12px 15px; border-bottom: 1px solid #dee2e6; display: flex; align-items: center; transition: background-color 0.2s; }
        .file-list li:last-child { border-bottom: none; }
        .file-list li:hover { background-color: #f1f3f5; }
        .file-list a { text-decoration: none; color: #495057; font-size: 1.1em; word-break: break-all; }
        .file-list .icon { margin-right: 15px; width: 24px; text-align: center; font-size: 1.4em; }
        .folder a { font-weight: bold; color: #0056b3; }
        #upload-progress-container { width: 100%; background-color: #e9ecef; border-radius: 5px; display: none; margin-top: 15px; }
		#upload-progress { width: 0%; height: 10px; background-color: #007bff; border-radius: 5px; transition: width 0.2s; }
    </style>
</head>
<body>
    <div class="container">
        
        <h2>Upload Files</h2>
        <div id="drop-zone" class="upload-box">
            <p>Drag & drop files here or click to select</p>
            <input type="file" id="file-input" multiple>
        </div>
        <div id="upload-progress-container"><div id="upload-progress"></div></div>

        <h2>Files</h2>
        <ul class="file-list">
            {{if .ParentDir}}
                <li class="folder"><span class="icon">📂</span><a href="{{.ParentDir}}?token={{.Token}}">.. (Parent Directory)</a></li>
            {{end}}
            {{range .Dirs}}
                <li class="folder"><span class="icon">📁</span><a href="{{.}}/?token={{$.Token}}">{{.}}</a></li>
            {{end}}
            {{range .Files}}
                <li><span class="icon">📄</span><a href="{{.}}?token={{$.Token}}">{{.}}</a></li>
            {{end}}
        </ul>
    </div>

    <script>
        const dropZone = document.getElementById('drop-zone');
        const fileInput = document.getElementById('file-input');
        const progressContainer = document.getElementById('upload-progress-container');
		const progressBar = document.getElementById('upload-progress');
		dropZone.addEventListener('click', () => fileInput.click());
        ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
            dropZone.addEventListener(eventName, preventDefaults, false);
            document.body.addEventListener(eventName, preventDefaults, false);
        });
        ['dragenter', 'dragover'].forEach(eventName => {
            dropZone.addEventListener(eventName, () => dropZone.classList.add('dragover'), false);
        });
        ['dragleave', 'drop'].forEach(eventName => {
            dropZone.addEventListener(eventName, () => dropZone.classList.remove('dragover'), false);
        });
        dropZone.addEventListener('drop', handleDrop, false);
        fileInput.addEventListener('change', (e) => handleFiles(e.target.files));
        function preventDefaults(e) { e.preventDefault(); e.stopPropagation(); }
        function handleDrop(e) { handleFiles(e.dataTransfer.files); }
        function handleFiles(files) {
            if (files.length === 0) return;
            progressContainer.style.display = 'block';
            progressBar.style.width = '0%';
            uploadFile(files[0]);
        }
        function uploadFile(file) {
            let url = '/upload?token={{.Token}}';
            let formData = new FormData();
            formData.append('file', file);
            let xhr = new XMLHttpRequest();
            xhr.open('POST', url, true);
            xhr.upload.addEventListener('progress', (e) => {
                let percent = (e.lengthComputable) ? (e.loaded / e.total) * 100 : 0;
                progressBar.style.width = percent + '%';
            });
            xhr.addEventListener('readystatechange', () => {
                if (xhr.readyState == 4 && xhr.status == 200) {
                    const newUrl = new URL(window.location.href);
                    newUrl.searchParams.set('token', '{{.Token}}');
                    window.location.href = newUrl.href;
                    window.location.reload();
                } else if (xhr.readyState == 4 && xhr.status != 200) {
                    alert('Upload failed: ' + xhr.statusText);
                    progressContainer.style.display = 'none';
                }
            });
            xhr.send(formData);
        }
    </script>
</body>
</html>
`

func init() {
	rootCmd.AddCommand(openCmd)
	allAddress := getIPs()
	defaultGateway := getGateway()
	var defaultAddress string
	if len(allAddress) > 0 {
		defaultAddress = allAddress[0]
		if defaultGateway != "" {
			if lastIndex := strings.LastIndex(defaultGateway, "."); lastIndex != -1 {
				pre := defaultGateway[:lastIndex]
				for _, address := range allAddress {
					if strings.HasPrefix(address, pre) {
						defaultAddress = address
						break
					}
				}
			}
		}
	} else {
		defaultAddress = "127.0.0.1"
	}
	openCmd.Flags().StringVarP(&inputAddress, "address", "a", defaultAddress, "set ip address")

	// Use crypto/rand for a secure random port number.
	randPort, err := rand.Int(rand.Reader, big.NewInt(3000))
	var portOffset int64
	if err != nil {
		// Fallback to a less random number if crypto/rand fails
		portOffset = time.Now().UnixMilli() % 3000
	} else {
		portOffset = randPort.Int64()
	}
	defaultPort := fmt.Sprintf("%d", 60000+portOffset)
	openCmd.Flags().StringVarP(&inputPort, "port", "p", defaultPort, "set port number")
}

func qrCode(urlBase, fileBase, token string) {
	path := "/"
	if fileBase != "" {
		path = "/" + fileBase
	}
	url := fmt.Sprintf("http://%s%s?token=%s", urlBase, path, token)

	fmt.Printf("\nScan the QR code to open file in mobile phone, or open this secure link in browser.\n")
	q, err := qrcode.New(url, qrcode.Low)
	if err != nil {
		fmt.Printf("could not generate QR code: %v\n", err)
		return
	}
	fmt.Printf(q.ToSmallString(false))
	sepLine := strings.Repeat("━", len(url)+2)
	fmt.Printf("\n┏%s┓\n┃ %s ┃\n┗%s┛\n", sepLine, url, sepLine)
}

func getIPs() (ips []string) {
	interfaceAddr, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Printf("fail to get net interface addrs: %v", err)
		return ips
	}
	for _, address := range interfaceAddr {
		if ipNet, isValidIpNet := address.(*net.IPNet); isValidIpNet && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP.String())
			}
		}
	}
	return ips
}

func getGateway() string {
	gw, err := gateway.DiscoverGateway()
	if err != nil {
		return ""
	}
	return gw.String()
}

func parsePath(path string) (string, string) {
	var fileBase, fileDir string
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("file or directory (%s) does not exist!\n", path)
		}
		os.Exit(1)
	}
	switch mode := fi.Mode(); {
	case mode.IsDir():
		fileBase = ""
		if dir, err := filepath.Abs(path); err != nil {
			panic(err)
		} else {
			fileDir = dir
		}
	case mode.IsRegular():
		fileBase = filepath.Base(path)
		if dir, err := filepath.Abs(filepath.Dir(path)); err != nil {
			panic(err)
		} else {
			fileDir = dir
		}
	}
	return fileDir, fileBase
}

func generateRandomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func panicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("FATAL: server crashed with panic: %v\n%s", err, string(debug.Stack()))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func tokenAuthMiddleware(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			http.NotFound(w, r)
			return
		}
		queryToken := r.URL.Query().Get("token")
		if queryToken == token {
			next.ServeHTTP(w, r)
		} else {
			http.Error(w, "Forbidden: Invalid or missing token.", http.StatusForbidden)
		}
	})
}

func serveFiles(urlBase, fileDir, token string) {
	appMux := http.NewServeMux()
	appMux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if part.FileName() == "" {
				continue
			}
			dst, err := os.Create(filepath.Join(fileDir, part.FileName()))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := io.Copy(dst, part); err != nil {
				dst.Close()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			dst.Close()
			log.Printf("Uploaded file: %s", part.FileName())
		}
		w.WriteHeader(http.StatusOK)
	})

	appMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fullPath := filepath.Join(fileDir, r.URL.Path)
		absFileDir, _ := filepath.Abs(fileDir)
		absFullPath, _ := filepath.Abs(fullPath)
		if !strings.HasPrefix(absFullPath, absFileDir) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		info, err := os.Stat(fullPath)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !info.IsDir() {
			http.ServeFile(w, r, fullPath)
			return
		}
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			http.Error(w, "Failed to read directory", http.StatusInternalServerError)
			return
		}
		var dirs, files []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, entry.Name())
			} else {
				files = append(files, entry.Name())
			}
		}
		sort.Strings(dirs)
		sort.Strings(files)
		var parentDir string
		if absFullPath != absFileDir {
			parentDir = filepath.Join(r.URL.Path, "..")
		}
		data := struct {
			Dirs, Files      []string
			ParentDir, Token string
		}{
			Dirs: dirs, Files: files, ParentDir: parentDir, Token: token,
		}
		tmpl, err := template.New("dir").Parse(htmlTemplate)
		if err != nil {
			log.Printf("Template parsing error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	finalHandler := panicMiddleware(tokenAuthMiddleware(appMux, token))

	log.Printf("Starting server. Access it at http://%s/?token=%s (Serving %s)", urlBase, token, fileDir)
	if err := http.ListenAndServe(urlBase, finalHandler); err != nil {
		panic(err)
	}
}
