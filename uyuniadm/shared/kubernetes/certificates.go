package kubernetes

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/uyuni-tools/shared/types"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
	"github.com/uyuni-project/uyuni-tools/uyuniadm/shared/templates"
	cmd_utils "github.com/uyuni-project/uyuni-tools/uyuniadm/shared/utils"
)

type TlsCert struct {
	RootCa      string
	Certificate string
	Key         string
}

// Install cert-manager and its CRDs using helm in the cert-manager namespace if needed
// and then create a self-signed CA and issuers.
// Returns helm arguments to be added to use the issuer
func installSslIssuers(globalFlags *types.GlobalFlags, helmFlags *cmd_utils.HelmFlags,
	sslFlags *cmd_utils.SslCertFlags, tlsCert *TlsCert, kubeconfig, fqdn string) []string {

	// Install cert-manager if needed
	installCertManager(globalFlags, helmFlags, kubeconfig)

	log.Info().Msg("Creating SSL certificate issuer")
	crdsDir, err := os.MkdirTemp("", "uyuniadm-*")
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to create temporary directory")
	}
	defer os.RemoveAll(crdsDir)

	issuerPath := filepath.Join(crdsDir, "issuer.yaml")

	issuerData := templates.IssuerTemplateData{
		Namespace:   helmFlags.Uyuni.Namespace,
		Country:     sslFlags.Country,
		State:       sslFlags.State,
		City:        sslFlags.City,
		Org:         sslFlags.Org,
		OrgUnit:     sslFlags.OU,
		Email:       sslFlags.Email,
		Fqdn:        fqdn,
		RootCa:      tlsCert.RootCa,
		Key:         tlsCert.Key,
		Certificate: tlsCert.Certificate,
	}

	if err = utils.WriteTemplateToFile(issuerData, issuerPath, 0500, true); err != nil {
		log.Fatal().Err(err).Msgf("Failed to generate issuer definition")
	}

	utils.RunCmd("kubectl", []string{"apply", "-f", issuerPath},
		"Failed to create issuer", globalFlags.Verbose)

	// Wait for issuer to be ready
	for i := 0; i < 60; i++ {
		out, err := exec.Command("kubectl", "get", "-o=jsonpath={.status.conditions[*].type}",
			"issuer", "uyuni-ca-issuer").Output()
		if err == nil && string(out) == "Ready" {
			return []string{"--set-json", "ingressSslAnnotations={\"cert-manager.io/issuer\": \"uyuni-ca-issuer\"}"}
		}
		time.Sleep(1 * time.Second)
	}
	log.Fatal().Msg("Issuer didn't turn ready after 60s")
	return []string{}
}

func installCertManager(globalFlags *types.GlobalFlags, helmFlags *cmd_utils.HelmFlags, kubeconfig string) {
	if !isDeploymentReady("", "cert-manager") {
		log.Info().Msg("Installing cert-manager")
		repo := ""
		chart := helmFlags.CertManager.Chart
		version := helmFlags.CertManager.Version
		namespace := helmFlags.CertManager.Namespace

		args := []string{
			"--set", "installCRDs=true",
			"--set-json", "global.commonLabels={\"installedby\": \"uyuniadm\"}",
		}
		extraValues := helmFlags.CertManager.Values
		if extraValues != "" {
			args = append(args, "-f", extraValues)
		}

		// Use upstream chart if nothing defined
		if chart == "" {
			repo = "https://charts.jetstack.io"
			chart = "cert-manager"
		}
		// The installedby label will be used to only uninstall what we installed
		helmUpgrade(globalFlags, kubeconfig, namespace, true, repo, "cert-manager", chart, version, args...)
	}

	// Wait for cert-manager to be ready
	waitForDeployment("", "cert-manager-webhook", "webhook")
}

func extractCaCertToConfig(verbose bool) {
	// TODO Replace with [trust-manager](https://cert-manager.io/docs/projects/trust-manager/) to automate this
	const jsonPath = "-o=jsonpath={.data.ca\\.crt}"

	log.Info().Msg("Extracting CA certificate to a configmap")
	// Skip extracting if the configmap is already present
	out, err := exec.Command("kubectl", "get", "configmap", "uyuni-ca", jsonPath).Output()
	log.Info().Msgf("CA cert: %s", string(out))
	if err == nil && len(out) > 0 {
		log.Info().Msg("uyuni-ca configmap already existing, skipping extraction")
		return
	}

	out, err = exec.Command("kubectl", "get", "secret", "uyuni-ca", jsonPath).Output()
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to get uyuni-ca certificate")
	}

	decoded, err := base64.StdEncoding.DecodeString(string(out))
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to base64 decode CA certificate")
	}

	message := fmt.Sprintf("Failed to create uyuni-ca config map from certificate: %s", err)
	valueArg := "--from-literal=ca.crt=" + string(decoded)
	utils.RunCmd("kubectl", []string{"create", "configmap", "uyuni-ca", valueArg}, message, verbose)
}
