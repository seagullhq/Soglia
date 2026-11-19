<#
.SYNOPSIS
    Carica il modello v6 nella cartella files/v6 del servizio Dokploy.

.DESCRIPTION
    Il compose di Dokploy monta ../files/v6 (rispetto al file docker-compose.yml)
    su /app/v6. Questo script crea la cartella remota, copia i file del modello
    con scp, sistema i permessi e mostra il contenuto finale.

.EXAMPLE
    .\scripts\upload-model.ps1 -Server root@192.0.2.10

.EXAMPLE
    .\scripts\upload-model.ps1 -Server root@192.0.2.10 -ServiceName altro-servizio -ModelPath D:\modelli\v6
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, HelpMessage = "Target SSH, es. root@192.0.2.10")]
    [string]$Server,

    [string]$ServiceName = 'feather-chat-soglia-thsnye',

    [string]$RemoteBase = '/etc/dokploy/compose',

    [string]$ModelPath
)

$ErrorActionPreference = 'Stop'

if (-not $ModelPath) {
    $ModelPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'v6'
}
$ModelPath = [System.IO.Path]::GetFullPath($ModelPath)
$Dest = "$RemoteBase/$ServiceName/files/v6"

$Required = @(
    'model_int8.onnx',
    'tokenizer.json',
    'calibration.json',
    'clean_blocklist.txt'
)

function Assert-Native {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "$Name non trovato. Installa OpenSSH Client e riprova."
    }
}

function Invoke-Remote {
    param([string]$Command)
    & ssh $Server $Command
    if ($LASTEXITCODE -ne 0) {
        throw "Comando remoto fallito: $Command"
    }
}

function Copy-ModelFile {
    param([string]$LocalFile)
    & scp $LocalFile "${Server}:${Dest}/"
    if ($LASTEXITCODE -ne 0) {
        throw "scp fallito per: $LocalFile"
    }
}

Assert-Native 'ssh'
Assert-Native 'scp'

if (-not (Test-Path -LiteralPath $ModelPath -PathType Container)) {
    throw "Cartella del modello non trovata: $ModelPath"
}

$missing = @($Required | Where-Object { -not (Test-Path -LiteralPath (Join-Path $ModelPath $_) -PathType Leaf) })
if ($missing.Count -gt 0) {
    throw "File obbligatori mancanti in $ModelPath : $($missing -join ', ')"
}

Write-Host "Modello locale : $ModelPath"
Write-Host "Destinazione   : ${Server}:${Dest}"
Write-Host ""

Invoke-Remote "mkdir -p '$Dest'"

$files = @(Get-ChildItem -LiteralPath $ModelPath -File | Sort-Object Name)
foreach ($file in $files) {
    $mb = [math]::Round($file.Length / 1MB, 1)
    Write-Host "Upload $($file.Name) ($mb MB)"
    Copy-ModelFile -LocalFile $file.FullName
}

Invoke-Remote "chmod -R a+rX '$Dest'"

Write-Host ""
Write-Host "---- contenuto remoto ----"
Invoke-Remote "ls -lh '$Dest'"

Write-Host ""
Write-Host "Modello caricato. Ora fai Deploy in Dokploy."
