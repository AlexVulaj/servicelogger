package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/geowa4/servicelogger/pkg/ocm"
	"github.com/geowa4/servicelogger/pkg/teaspoon"
	"github.com/geowa4/servicelogger/pkg/templates"
	sdk "github.com/openshift-online/ocm-sdk-go"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"os"
	"sync"
)

var sendServiceLogCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a service log",
	Long: `Send service log to the customer from JSON template passed via stdin

Example: servicelogger search | servicelogger send -u 'https://api.openshift.com' -t \"$(ocm token)\" -c $CLUSTER_ID"`,
	Args: cobra.NoArgs,
	PreRun: func(cmd *cobra.Command, args []string) {
		_ = viper.BindPFlag("ocm_url", cmd.Flags().Lookup("ocm-url"))
		_ = viper.BindPFlag("ocm_token", cmd.Flags().Lookup("ocm-token"))
		_ = viper.BindPFlag("cluster_id", cmd.Flags().Lookup("cluster-id"))
		_ = viper.BindPFlag("cluster_ids", cmd.Flags().Lookup("cluster-ids"))
	},
	Run: func(cmd *cobra.Command, args []string) {
		if !viper.IsSet("cluster_ids") {
			viper.Set("cluster_ids", []string{viper.GetString("cluster_id")})
		}
		cobra.CheckErr(checkRequiredArgsExist("ocm_url", "ocm_token", "cluster_ids"))

		var template templates.Template
		input, err := io.ReadAll(os.Stdin)
		cobra.CheckErr(err)
		err = json.Unmarshal(input, &template)
		cobra.CheckErr(err)
		fmt.Println(teaspoon.RenderMarkdown(template.String()))

		clusterIds := viper.GetStringSlice("cluster_ids")
		confirmation := false
		err = huh.NewForm(huh.NewGroup(huh.NewConfirm().Value(&confirmation).Title(fmt.Sprintf("Send this service log to %v cluster(s)?", len(clusterIds))).Affirmative("Send").Negative("Cancel"))).Run()
		if err != nil {
			return
		}

		if confirmation {
			ctx, cancel := context.WithCancel(context.Background())
			var serviceLogWaitGroup sync.WaitGroup
			for _, clusterId := range clusterIds {
				serviceLogWaitGroup.Add(1)
				go func(cId string) {
					defer serviceLogWaitGroup.Done()

					errSendSL := sendServiceLog(
						viper.GetString("ocm_url"),
						viper.GetString("ocm_token"),
						cId,
						template,
					)

					result := fmt.Sprintf("%s\t", cId)
					if errSendSL == nil {
						result += "success"
					} else {
						result += fmt.Sprintf("failure\t%v", errSendSL.Error())
					}

					fmt.Println(result)
				}(clusterId)
			}

			done := make(chan bool)
			go func() {
				_ = spinner.New().Title("Sending service log").Context(ctx).Run()
				done <- true
			}()

			serviceLogWaitGroup.Wait()
			cancel()
			<-done
		} else {
			_, _ = fmt.Fprint(os.Stderr, "Service log canceled")
		}
	},
}

func init() {
	sendServiceLogCmd.Flags().StringP("ocm-url", "u", "https://api.openshift.com", "OCM URL (falls back to $OCM_URL and then 'https://api.openshift.com')")
	sendServiceLogCmd.Flags().StringP("ocm-token", "t", "", "OCM token (falls back to $OCM_TOKEN)")
	sendServiceLogCmd.Flags().StringP("cluster-id", "c", "", "internal cluster ID (defaults to $CLUSTER_ID)")
	sendServiceLogCmd.Flags().StringSlice("cluster-ids", nil, "internal cluster IDs (defaults to $CLUSTER_IDS, space separated)")

	sendServiceLogCmd.MarkFlagsMutuallyExclusive("cluster-id", "cluster-ids")

	rootCmd.AddCommand(sendServiceLogCmd)
}

func sendServiceLog(url, token, clusterId string, t templates.Template) error {
	conn, err := ocm.NewConnectionWithTemporaryToken(url, token)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error creating ocm connection: %q", err)
	}
	defer func(conn *sdk.Connection) {
		_ = conn.Close()
	}(conn)
	client := ocm.NewClient(conn)
	err = client.PostServiceLog(clusterId, t)
	return err
}
