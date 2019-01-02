package cmd

import (
	"fmt"
	"os/user"

	"github.com/spf13/cobra"
)

// testCmd represents the test command
var testCmd = &cobra.Command{
	Use:   "hello",
	Short: "A testing command",
	Long:  `Just for testing`,
	Run: func(cmd *cobra.Command, args []string) {
		user, err := user.Current()
		if err != nil {
			fmt.Println("Hello")
		} else {
			fmt.Println("Hi, " + user.Username)
		}

	},
}

func init() {
	rootCmd.AddCommand(testCmd)
}
