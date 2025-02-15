package kubernetes

import (
	"fmt"
	
	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/uyuni-tools/shared/types"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
	cmd_utils "github.com/uyuni-project/uyuni-tools/uyuniadm/shared/utils"
)

const HELM_APP_NAME = "uyuni"

func Deploy(globalFlags *types.GlobalFlags, imageFlags *cmd_utils.ImageFlags,
	helmFlags *cmd_utils.HelmFlags, sslFlags *cmd_utils.SslCertFlags, clusterInfos *ClusterInfos,
	fqdn string, helmArgs ...string) {

	// If installing on k3s, install the traefik helm config in manifests
	isK3s := clusterInfos.IsK3s()
	IsRke2 := clusterInfos.IsRke2()
	if isK3s {
		InstallK3sTraefikConfig()
	} else if IsRke2 {
		InstallRke2NginxConfig(helmFlags.Uyuni.Namespace)
	}

	// Install the uyuni server helm chart
	UyuniUpgrade(globalFlags, imageFlags, helmFlags, clusterInfos.GetKubeconfig(), fqdn, clusterInfos.Ingress, helmArgs...)

	// Wait for the pod to be started
	waitForDeployment(helmFlags.Uyuni.Namespace, HELM_APP_NAME, "uyuni")
	utils.WaitForServer(globalFlags, "")
}

func DeployCertificate(globalFlags *types.GlobalFlags, helmFlags *cmd_utils.HelmFlags,
	sslFlags *cmd_utils.SslCertFlags, ca *TlsCert, kubeconfig string, fqdn string) []string {

	helmArgs := []string{}
	if sslFlags.UseExisting {
		// TODO Check that we have the expected secret and config in place
	} else {
		// Install cert-manager and a self-signed issuer ready for use
		issuerArgs := installSslIssuers(globalFlags, helmFlags, sslFlags, ca, kubeconfig, fqdn)
		helmArgs = append(helmArgs, issuerArgs...)
	}

	// Extract the CA cert into uyuni-ca config map as the container shouldn't have the CA secret
	extractCaCertToConfig(globalFlags.Verbose)

	return helmArgs
}

func UyuniUpgrade(globalFlags *types.GlobalFlags, imageFlags *cmd_utils.ImageFlags,
	helmFlags *cmd_utils.HelmFlags, kubeconfig string,
	fqdn string, ingress string, helmArgs ...string) {

	log.Info().Msg("Installing Uyuni")

	// The guessed ingress is passed before the user's value to let the user override it in case we got it wrong.
	helmParams := []string{
		"--set", "ingress=" + ingress,
	}

	extraValues := helmFlags.Uyuni.Values
	if extraValues != "" {
		helmParams = append(helmParams, "-f", extraValues)
	}

	// The values computed from the command line need to be last to override what could be in the extras
	helmParams = append(helmParams,
		"--set", fmt.Sprintf("images.server=%s:%s", imageFlags.Name, imageFlags.Tag),
		"--set", "fqdn="+fqdn)

	helmParams = append(helmParams, helmArgs...)

	namespace := helmFlags.Uyuni.Namespace
	chart := helmFlags.Uyuni.Chart
	version := helmFlags.Uyuni.Version
	helmUpgrade(globalFlags, kubeconfig, namespace, true, "", HELM_APP_NAME, chart, version, helmParams...)
}
