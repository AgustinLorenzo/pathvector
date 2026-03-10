package cmd

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/AgustinLorenzo/pathvector/pkg/bird"
)

var (
	allowCommands		[]string
	commentMessage		string
)

func init() {
	protocolCmd.Flags().StringVarP(&commentMessage, "message", "m", "", "enable/disable custom message")
	rootCmd.AddCommand(protocolCmd)
}

// pathvectorPeer holds the minimal fields needed from each peer in the YAML
type pathvectorPeer struct {
	ASN int `yaml:"asn"`
}

// pathvectorYAML holds a minimal view of pathvector.yaml
type pathvectorYAML struct {
	Peers map[string]pathvectorPeer `yaml:"peers"`
}

// birdProtocolPrefix generates the BIRD protocol name prefix for a given peer.
// Pathvector generates: <PEER_NAME_UPPERCASE>_AS<ASN>
// Example: "LOCIX_Dusseldorf_4" with ASN 202409 → "LOCIX_DUSSELDORF_4_AS202409"
// BIRD then appends _v4 or _v6, and _1, _2... for multiple neighbors.
func birdProtocolPrefix(peerName string, asn int) string {
	return fmt.Sprintf("%s_AS%d", strings.ToUpper(peerName), asn)
}

// resolveBirdProtocols queries BIRD directly and returns all protocol names
// that match the prefix for the given peer name.
func resolveBirdProtocols(configFile, name string, birdSocket string) ([]string, error) {
	if name == "all" {
		return []string{"all"}, nil
	}

	// Read pathvector YAML to get the ASN
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Warnf("Could not read pathvector config: %s", err)
		return []string{name}, nil
	}

	var cfg pathvectorYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Warnf("Could not parse pathvector config: %s", err)
		return []string{name}, nil
	}

	peer, ok := cfg.Peers[name]
	if !ok {
		// Not found in YAML — assume it's already a raw BIRD protocol name
		log.Debugf("Peer %q not found in pathvector config, using as raw BIRD protocol name", name)
		return []string{name}, nil
	}

	if peer.ASN == 0 {
		log.Warnf("Peer %q found in config but has no ASN defined, using peer name as-is", name)
		return []string{name}, nil
	}

	prefix := birdProtocolPrefix(name, peer.ASN)
	log.Debugf("Looking for BIRD protocols with prefix %q", prefix)

	// Query BIRD for all protocols and filter by prefix
	output, _, err := bird.RunCommand("show protocols", birdSocket)
	if err != nil {
		log.Warnf("Could not query BIRD protocols, falling back to prefix: %s", err)
		return []string{prefix}, nil
	}

	var matched []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		protoName := fields[0]
		if strings.HasPrefix(protoName, prefix) {
			matched = append(matched, protoName)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no BIRD protocols found with prefix %q (peer %q)", prefix, name)
	}

	log.Debugf("Resolved peer %q to BIRD protocols: %v", name, matched)
	return matched, nil
}

var protocolCmd = &cobra.Command{
	Use:     "protocol <(r)estart|re(l)oad|(e)nable|(d)isable> <peer name>",
	Args:	 func(cmd *cobra.Command, args []string) error {
				if len(args) < 1 {
					log.Fatal("requires a command <restart|reload|enable|disable>")
				} else if !allowCommand(args[0]) {
					log.Fatal("This command is not allowed: ", args[0])
				} else if len(args) < 2 {
					log.Fatal("requires protocol name")
				}
				return nil
			 },
	Aliases: []string{"p", "protocols"},
	Short:   "Protocol command (restart, reload, enable or disable protocol sessions)",
	Long:    "With this command you can restart, reload, enable or disable a protocol. Pass the peer name as defined in pathvector.yaml and it will be resolved automatically to all matching BIRD protocol names (including multiple neighbors). You can also pass \"all\" to affect all sessions.",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := loadConfig()
		if err != nil {
			log.Warnf("Error loading config, falling back to no-config output parsing: %s", err)
		}

		// Resolve peer name to all matching BIRD protocol names
		birdProtocols, err := resolveBirdProtocols(configFile, args[1], c.BIRDSocket)
		if err != nil {
			log.Fatalf("Error resolving peer name: %s", err)
		}

		log.Infof("Starting bird protocol command for %d protocol(s)", len(birdProtocols))

		hasError := false
		for _, birdProtocol := range birdProtocols {
			resolvedArgs := []string{args[0], birdProtocol}
			commandOutput, _, err := bird.RunCommand(runCMD(resolvedArgs, commentMessage), c.BIRDSocket)
			if err != nil {
				log.Errorf("Error running command for %s: %s", birdProtocol, err)
				hasError = true
				continue
			} else if strings.Contains(commandOutput, "syntax error") ||
				strings.Contains(commandOutput, "unexpected CF_SYM_UNDEFINED") ||
				strings.Contains(commandOutput, "expecting END or CF_SYM_KNOWN or TEXT") {
				log.Errorf("Protocol %q not found in BIRD", birdProtocol)
				hasError = true
				continue
			}
			log.Debugf("Command Output: %s", commandOutput)
			fmt.Printf("Command %s succeeded for BIRD protocol: %s\n", args[0], birdProtocol)
		}

		if hasError {
			log.Fatal("One or more protocols failed")
		}
	},
}

// allowCommand check if this command is allowed to run
func allowCommand(cmd string) bool {
	allowCommands := []string{"restart", "reload", "enable", "disable", "r", "l", "e", "d"}
	for _, allowed := range allowCommands {
		if allowed == cmd {
			return true
		}
	}
	return false
}

// runCMD generate the run command
func runCMD(args []string, message string) string {
	switch args[0] {
		case "d":
			args[0] = "disable"
			break
		case "e":
			args[0] = "enable"
			break
		case "r":
			args[0] = "restart"
			break
		case "l":
			args[0] = "reload"
			break
	}

	if (args[0] == "disable" || args[0] == "enable") && message != "" {
		return args[0] + " " + args[1] + " \"" + message + "\""
	} else if args[0] == "disable" {
	 	return args[0] + " " + args[1] + " \"Protocol manually " + args[0] + "d by pathvector\""
	}

	return args[0] + " " + args[1]
}