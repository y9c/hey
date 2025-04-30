package cmd

import (
	"fmt"
	"os"

	cc "github.com/ivanpirog/coloredcobra"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hey",
	Short: "HeY may short for Yc @ Helab",
	Long: `Some useful/useless commands
  from Yc @ Helab
  ---------------`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			// return errors.New("requires at least one sub command")
			_ = cmd.Help()
			os.Exit(0)
		}
	},
}

func Execute() {
	cc.Init(&cc.Config{
		RootCmd:  rootCmd,
		Headings: cc.Yellow + cc.Bold + cc.Underline,
		Commands: cc.Green + cc.Bold,
		Example:  cc.Italic,
		ExecName: cc.Bold,
		Flags:    cc.Blue + cc.Bold,
	})
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
