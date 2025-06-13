package cmd

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
		Short: "Open file or directory in a browser with a beautiful server UI",
		Long: `Serves a file or directory with a modern web interface.
When serving a directory, it provides a beautiful file listing and supports drag-and-drop file uploads.
A QR code is also generated for easy access from mobile devices.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("requires a file or directory path")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			urlBase := fmt.Sprintf("%s:%s", inputAddress, inputPort)
			fileDir, fileBase := parsePath(args[0])
			qrCode(urlBase, fileBase)
			serveFiles(urlBase, fileDir)
		},
	}
)

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Hey! File Server</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; margin: 0; background-color: #f8f9fa; color: #343a40; }
        .container { max-width: 800px; margin: 40px auto; padding: 0 20px; }
        h1, h2 { color: #007bff; border-bottom: 2px solid #dee2e6; padding-bottom: 10px; }
        .upload-box { border: 3px dashed #007bff; border-radius: 10px; padding: 40px; text-align: center; margin-bottom: 30px; background-color: #fff; transition: background-color 0.2s ease-in-out; cursor: pointer; }
        .upload-box p { margin: 0 0 15px 0; font-size: 1.2em; }
        .upload-box:hover, .upload-box.dragover { background-color: #e9ecef; }
        #file-input { display: none; }
        .file-list { list-style: none; padding: 0; border: 1px solid #dee2e6; border-radius: 5px; background-color: #fff; }
        .file-list li { padding: 12px 15px; border-bottom: 1px solid #dee2e6; display: flex; align-items: center; transition: background-color 0.2s; }
        .file-list li:last-child { border-bottom: none; }
        .file-list li:hover { background-color: #f1f3f5; }
        .file-list a { text-decoration: none; color: #495057; font-size: 1.1em; }
        .file-list .icon { margin-right: 15px; width: 24px; text-align: center; font-size: 1.4em; }
        .folder a { font-weight: bold; color: #0056b3; }
        #upload-progress-container { width: 100%; background-color: #e9ecef; border-radius: 5px; display: none; margin-top: 15px; }
		#upload-progress { width: 0%; height: 10px; background-color: #007bff; border-radius: 5px; transition: width 0.2s; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Hey! File Server</h1>
        
        <h2>Upload Files</h2>
        <div id="drop-zone" class="upload-box">
            <p>Drag & drop files here or click to select</p>
            <input type="file" id="file-input" multiple>
        </div>
        <div id="upload-progress-container"><div id="upload-progress"></div></div>

        <h2>Files</h2>
        <ul class="file-list">
            {{if .ParentDir}}
                <li class="folder"><span class="icon">&#128194;</span><a href="{{.ParentDir}}">.. (Parent Directory)</a></li>
            {{end}}
            {{range .Dirs}}
                <li class="folder"><span class="icon">&#128193;</span><a href="{{.}}/">{{.}}</a></li>
            {{end}}
            {{range .Files}}
                <li><span class="icon">&#128196;</span><a href="{{.}}">{{.}}</a></li>
            {{end}}
        </ul>
    </div>

    <script>
        const dropZone = document.getElementById('drop-zone');
        const fileInput = document.getElementById('file-input');
        const progressContainer = document.getElementById('upload-progress-container');
		const progressBar = document.getElementById('upload-progress');

		// Make the entire dropzone clickable
		dropZone.addEventListener('click', () => fileInput.click());

        // Prevent default drag behaviors
        ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
            dropZone.addEventListener(eventName, preventDefaults, false);
            document.body.addEventListener(eventName, preventDefaults, false);
        });

        // Highlight drop zone when item is dragged over it
        ['dragenter', 'dragover'].forEach(eventName => {
            dropZone.addEventListener(eventName, () => dropZone.classList.add('dragover'), false);
        });
        ['dragleave', 'drop'].forEach(eventName => {
            dropZone.addEventListener(eventName, () => dropZone.classList.remove('dragover'), false);
        });

        // Handle dropped files
        dropZone.addEventListener('drop', handleDrop, false);

        // Handle file selection from input
        fileInput.addEventListener('change', (e) => handleFiles(e.target.files));

        function preventDefaults(e) {
            e.preventDefault();
            e.stopPropagation();
        }

        function handleDrop(e) {
            let dt = e.dataTransfer;
            let files = dt.files;
            handleFiles(files);
        }

        function handleFiles(files) {
            if (files.length === 0) return;
            progressContainer.style.display = 'block';
            progressBar.style.width = '0%';
            uploadFile(files[0]); // For simplicity, this example uploads one file at a time from a selection
        }

        function uploadFile(file) {
            let url = '/upload';
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
                    location.reload(); // Success, reload page to show new file
                } else if (xhr.readyState == 4 && xhr.status != 200) {
                    alert('Upload failed.');
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
		pre := defaultGateway[:strings.LastIndex(defaultGateway, ".")]
		defaultAddress = allAddress[0]
		for _, address := range allAddress {
			if strings.HasPrefix(address, pre) {
				defaultAddress = address
				break
			}
		}
	} else {
		defaultAddress = "127.0.0.1"
	}

	openCmd.Flags().StringVarP(&inputAddress, "address", "a", defaultAddress, "set ip address")
	rand.Seed(time.Now().UnixNano())
	defaultPort := fmt.Sprintf("%d", 60000+rand.Intn(3000))
	openCmd.Flags().StringVarP(&inputPort, "port", "p", defaultPort, "set port number")
}

func qrCode(urlBase, fileBase string) {
	url := fmt.Sprintf("http://%s/%s", urlBase, fileBase)
	fmt.Printf("\nScan the QR code to open file in mobile phone, or open this link in browser.\n")
	q, err := qrcode.New(url, qrcode.Low)
	if err != nil {
		fmt.Printf("%v\n", err)
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
			fmt.Printf("file (%s) does not exist!\n", path)
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

func serveFiles(urlBase, fileDir string) {
	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fullPath := filepath.Join(fileDir, r.URL.Path)
		info, err := os.Stat(fullPath)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		} else if err != nil {
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

		parentDir := ""
		if fullPath != fileDir {
			parentDir = filepath.Dir(r.URL.Path)
		}

		data := struct {
			Dirs      []string
			Files     []string
			ParentDir string
		}{
			Dirs:      dirs,
			Files:     files,
			ParentDir: parentDir,
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

	log.Printf("Serving %s on http://%s\n", fileDir, urlBase)
	if err := http.ListenAndServe(urlBase, nil); err != nil {
		panic(err)
	}
}
