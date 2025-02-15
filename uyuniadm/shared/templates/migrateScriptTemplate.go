package templates

import (
	"io"
	"text/template"
)

const podmanMigrationScriptTemplate = `#!/bin/bash
set -e
for folder in {{ range .Volumes }}{{ . }} {{ end }};
do
  echo "Copying $folder..."
  rsync -e "ssh -A " --rsync-path='sudo rsync' -avz {{ .SourceFqdn }}:$folder/ $folder;
done;
rm -f /srv/www/htdocs/pub/RHN-ORG-TRUSTED-SSL-CERT;
ln -s /etc/pki/trust/anchors/LOCAL-RHN-ORG-TRUSTED-SSL-CERT /srv/www/htdocs/pub/RHN-ORG-TRUSTED-SSL-CERT;

echo "Extracting time zone..."
ssh {{ .SourceFqdn }} timedatectl show -p Timezone >/var/lib/uyuni-tools/data

{{ if .Kubernetes }}
echo "Altering configuration for kubernetes..."
echo 'server.no_ssl = 1' >> /etc/rhn/rhn.conf;
sed 's/address=[^:]*:/address=*:/' -i /etc/rhn/taskomatic.conf;

if test ! -f /etc/tomcat/conf.d/remote_debug.conf -a -f /etc/sysconfig/tomcat; then
  mv /etc/sysconfig/tomcat /etc/tomcat/conf.d/remote_debug.conf
fi

sed 's/address=[^:]*:/address=*:/' -i /etc/tomcat/conf.d/remote_debug.conf

if test -d /root/ssl-build; then
  echo "Extracting SSL CA certificate..."
  # Extract the SSL CA certificate and key.
  # The server certificate will be auto-generated by cert-manager using it, so no need to copy it.
  cp /root/ssl-build/RHN-ORG-TRUSTED-SSL-CERT /var/lib/uyuni-tools/
  cp /root/ssl-build/RHN-ORG-PRIVATE-SSL-KEY /var/lib/uyuni-tools/
else
  echo "Extracting SSL certificate..."
  # For third party certificates, the CA chain is in the certificate file.
  scp -A {{ .SourceFqdn }}:/etc/pki/tls/private/spacewalk.key /var/lib/uyuni-tools/
  scp -A {{ .SourceFqdn }}:/etc/pki/tls/certs/spacewalk.crt /var/lib/uyuni-tools/
fi
{{ end }}
echo "DONE"`

type MigrateScriptTemplateData struct {
	Volumes    map[string]string
	SourceFqdn string
	Kubernetes bool
}

func (data MigrateScriptTemplateData) Render(wr io.Writer) error {
	t := template.Must(template.New("script").Parse(podmanMigrationScriptTemplate))
	return t.Execute(wr, data)
}
