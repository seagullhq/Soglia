# Examples

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

```powershell
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
# -SkipCertificateCheck avoids the local-CA warning; import certs\ca.crt to trust it.
Invoke-RestMethod -Uri https://localhost:8080/auth/login -Method Post -WebSession $session `
  -SkipCertificateCheck -Body '{"username":"admin","password":"your-password"}' -ContentType 'application/json'

Invoke-RestMethod -Uri https://localhost:8080/moderate -Method Post -WebSession $session `
  -SkipCertificateCheck -Body '{"text":"You are such a f**king idiot"}' -ContentType 'application/json'

Invoke-RestMethod -Uri https://localhost:8080/moderate/batch -Method Post -WebSession $session `
  -SkipCertificateCheck -Body '{"texts":["Ok","stfu you absolute m0r0n"]}' -ContentType 'application/json'
```

```bash
curl -s --ssl-no-revoke --cacert certs/ca.crt -c cookies.txt -X POST https://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'

curl -s --ssl-no-revoke --cacert certs/ca.crt -b cookies.txt -X POST https://localhost:8080/moderate \
  -H 'Content-Type: application/json' \
  -d '{"text":"Ciao! Ci vediamo domani?"}'
```
