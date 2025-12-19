package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/fatih/color"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/AgustinLorenzo/pathvector/pkg/bird"
	"github.com/AgustinLorenzo/pathvector/pkg/templating"
	"github.com/AgustinLorenzo/pathvector/pkg/util"
)

var (
	realProtocolNames bool
	onlyBGP           bool
	showTags          bool
	tagFilter         []string
)

func init() {
	statusCmd.Flags().BoolVarP(&realProtocolNames, "real-protocol-names", "r", false, "use real protocol names")
	statusCmd.Flags().BoolVarP(&onlyBGP, "bgp", "b", false, "only show BGP protocols")
	statusCmd.Flags().BoolVar(&showTags, "tags", false, "show tags column")
	statusCmd.Flags().StringArrayVarP(&tagFilter, "filter", "f", []string{}, "tags to filter by")
	rootCmd.AddCommand(statusCmd)
}

// Define the prefix order for sorting (case insensitive)
var prefixOrder = []string{
	"internal",
	"transit",
	"collector",
	"rs",
	"peer",
	"client",
	"downstream",
}

// getPrefixPriority returns the priority of a protocol name based on its prefix (case insensitive)
func getPrefixPriority(name string) int {
	lowerName := strings.ToLower(name)
	for i, prefix := range prefixOrder {
		if strings.HasPrefix(lowerName, prefix) {
			return i
		}
	}
	// If no prefix matches, put it at the end
	return len(prefixOrder)
}

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"s", "status"},
	Short:   "Show protocol status",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := loadConfig()
		if err != nil {
			log.Warnf("Error loading config, falling back to no-config output parsing: %s", err)
		}

		commandOutput, _, err := bird.RunCommand("show protocols all", c.BIRDSocket)
		if err != nil {
			log.Fatal(err)
		}

		// Read protocol names map
		var protocols map[string]*templating.Protocol
		if !realProtocolNames {
			contents, err := os.ReadFile(path.Join("/etc/bird/", "protocols.json"))
			if err != nil {
				log.Fatalf("Reading protocol names: %v", err)
			}
			if err := json.Unmarshal(contents, &protocols); err != nil {
				log.Fatalf("Unmarshalling protocol names: %v", err)
			}
		}

		protocolStates, err := bird.ParseProtocols(commandOutput)
		if err != nil {
			log.Fatal(err)
		}

		// Sort protocol states: non-BGP first (in original order), then BGP (by prefix and alphabetically)
		sort.SliceStable(protocolStates, func(i, j int) bool {
			// Check if protocols are BGP or non-BGP
			iIsBGP := protocolStates[i].BGP != nil
			jIsBGP := protocolStates[j].BGP != nil

			// Non-BGP protocols always come first
			if !iIsBGP && jIsBGP {
				return true
			}
			if iIsBGP && !jIsBGP {
				return false
			}

			// If both are non-BGP, keep original order (stable sort handles this)
			if !iIsBGP && !jIsBGP {
				return false
			}

			// Both are BGP protocols - sort by prefix priority, then alphabetically
			priorityI := getPrefixPriority(protocolStates[i].Name)
			priorityJ := getPrefixPriority(protocolStates[j].Name)

			// First compare by prefix priority
			if priorityI != priorityJ {
				return priorityI < priorityJ
			}

			// If same prefix priority, sort alphabetically (case insensitive)
			return strings.ToLower(protocolStates[i].Name) < strings.ToLower(protocolStates[j].Name)
		})

		header := []string{"Peer", "AS", "Neighbor", "State", "In", "Out", "Since", "Info"}
		if showTags {
			header = append(header, "Tags")
		}
		util.PrintTable(header, func() [][]string {
			var table [][]string
			for _, protocolState := range protocolStates {
				if !onlyBGP || protocolState.BGP != nil {
					neighborAddr, neighborAS := "-", "-"
					if protocolState.BGP != nil {
						neighborAS = parseTableInt(protocolState.BGP.NeighborAS)
						neighborAddr = protocolState.BGP.NeighborAddress
					}

					// Lookup peer in protocol JSON
					protocolName := protocolState.Name
					var tags []string
					if p, found := protocols[protocolState.Name]; found {
						protocolName = p.Name
						tags = p.Tags
					}

					if len(tagFilter) == 0 || containsAny(tagFilter, tags) {
						row := []string{
							protocolName,
							neighborAS,
							neighborAddr,
							colorStatus(protocolState.State),
							parseTableInt(protocolState.Routes.Imported),
							parseTableInt(protocolState.Routes.Exported),
							protocolState.Since,
							colorStatus(protocolState.Info),
						}
						if showTags {
							row = append(row, strings.Join(tags, ", "))
						}
						table = append(table, row)
					}
				}
			}
			return table
		}())
	},
}

// containsAny checks if two string slices contain any of the same elements
func containsAny(a []string, b []string) bool {
	for _, i := range a {
		for _, j := range b {
			if i == j {
				return true
			}
		}
	}
	return false
}

func parseTableInt(i int64) string {
	if i == -1 {
		return ""
	}
	return fmt.Sprintf("%d", i)
}

func colorStatus(s string) string {
	if s == "up" || s == "Established" {
		return color.GreenString(s)
	} else if strings.Contains(s, "Error") || s == "down" {
		return color.RedString(s)
	} else if strings.Contains(s, "Connect") || s == "start" {
		return color.YellowString(s)
	}
	return s
}