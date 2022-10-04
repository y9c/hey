package cmd

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
		Use:   "open",
		Short: "Open file in server with browser",
		Long: `Open file in server with browser
The url is generated for the file/directory
The QR code is for openning file in browser`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("requires at least one arg")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			urlBase := fmt.Sprintf("%s:%s", inputAddress, inputPort)
			//url2show = fmt.Sprintf("http://%s:%s/%s", url, file)
			fileDir, fileBase := parsePath(args[0])
			qrCode(urlBase, fileBase)
			serveFiles(urlBase, fileDir)
		},
	}
)

func init() {

	rootCmd.AddCommand(openCmd)

	allAddress := getIPs()
	defaultGateway := getGateway()

	pre := defaultGateway[:strings.LastIndex(defaultGateway, ".")]
	defaultAddress := allAddress[0]
	for _, address := range allAddress {
		if strings.HasPrefix(address, pre) {
			defaultAddress = address
		}
	}

	openCmd.Flags().StringVarP(&inputAddress, "address", "a", defaultAddress, "set ip address")

	rand.Seed(time.Now().UnixNano())
	defaultPort := fmt.Sprintf("%d", 50000+rand.Intn(1000))
	openCmd.Flags().StringVarP(&inputPort, "port", "p", defaultPort, "set port number")

}

func qrCode(urlBase, fileBase string) {

	url := fmt.Sprintf("http://%s/%s", urlBase, fileBase)
	fmt.Printf("\nScan the QR code to open file in mobile phone, or open the this link in browser.\n")
	//	"github.com/mdp/qrterminal/v3"
	// QR code
	// config := qrterminal.Config{
	// 	Level:     qrterminal.L,
	// 	Writer:    os.Stdout,
	// 	BlackChar: qrterminal.BLACK,
	// 	WhiteChar: qrterminal.WHITE,
	// 	QuietZone: 1,
	// }
	// qrterminal.GenerateWithConfig(url, config)

	// QR code
	q, err := qrcode.New(url, qrcode.Low)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	fmt.Printf(q.ToSmallString(false))

	sepLine := strings.Repeat("━", len(url)+2)
	// URL link
	fmt.Printf("\n┏%s┓\n┃ %s ┃\n┗%s┛\n", sepLine, url, sepLine)

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

func getGateway() string {
	gw, err := gateway.DiscoverGateway()
	if err != nil {
		panic(err)
	}
	return gw.String()
}

func parsePath(path string) (string, string) {

	// file path

	var fileBase string
	var fileDir string

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

	http.Handle("/", http.FileServer(http.Dir(fileDir)))
	if err := http.ListenAndServe(urlBase, nil); err != nil {
		panic(err)
	}

}
