package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mdp/qrterminal"
)

var (
	h bool

	a       string
	p       string
	content string
	url     string
)

func init() {
	flag.BoolVar(&h, "h", false, "this help")

	rand.Seed(time.Now().UnixNano())
	port := fmt.Sprintf(":%d", 10000+rand.Intn(1000))
	flag.StringVar(&p, "p", port, "port")

	address := fmt.Sprintf("http://%s", getIPs()[0])
	flag.StringVar(&a, "a", address, "address")

	// 改变默认的 Usage
	flag.Usage = usage
}

func main() {
	flag.Parse()

	if h {
		flag.Usage()
	} else {
		if flag.NArg() == 0 {
			content = "./"
		} else if flag.NArg() == 1 {
			content = flag.Args()[0]
		} else {
			content = flag.Args()[0]
			fmt.Println("More than one argument passed, only the first one was used!")
		}
		fi, err := os.Stat(content)
		if err != nil {
			fmt.Println(err)
			return
		}
		switch mode := fi.Mode(); {
		case mode.IsDir():
			url = fmt.Sprintf("%s%s", a, p)
		case mode.IsRegular():
			file := filepath.Base(content)
			url = fmt.Sprintf("%s%s/%s", a, p, file)
		}
		dir, err := filepath.Abs(filepath.Dir(content))
		if err != nil {
			panic(err)
		}

		// QR code
		config := qrterminal.Config{
			Level:     qrterminal.M,
			Writer:    os.Stdout,
			BlackChar: qrterminal.BLACK,
			WhiteChar: qrterminal.WHITE,
			QuietZone: 1,
		}
		qrterminal.GenerateWithConfig(url, config)
		fmt.Printf("\n\n---------------\n%s\n---------------\n", url)

		// httpserver
		http.Handle("/", http.FileServer(http.Dir(dir)))
		if err := http.ListenAndServe(p, nil); err != nil {
			panic(err)
		}

	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `hey version: 0.0.1
Usage: hey_open [-h] [-a address] [-p port] [file/dir]

Options:
`)
	flag.PrintDefaults()
}

func getIPs() (ips []string) {

	interfaceAddr, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Printf("fail to get net interface addrs: %v", err)
		return ips
	}

	for _, address := range interfaceAddr {
		ipNet, isValidIpNet := address.(*net.IPNet)
		if isValidIpNet && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP.String())
			}
		}
	}
	return ips
}
