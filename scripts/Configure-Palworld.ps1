param(
    [string]$ServerDir = 'D:\PalworldServer\server',
    [string]$AdminPassword = '1234',
    [string]$ServerPassword = '1234',
    [ValidateRange(1, 32)]
    [int]$Players = 4
)

$ErrorActionPreference = 'Stop'
$source = Join-Path $ServerDir 'DefaultPalWorldSettings.ini'
$configDir = Join-Path $ServerDir 'Pal\Saved\Config\WindowsServer'
$destination = Join-Path $configDir 'PalWorldSettings.ini'

if (-not (Test-Path -LiteralPath $source)) {
    throw "DefaultPalWorldSettings.ini was not found at $source"
}
if ($AdminPassword.Contains('"') -or $ServerPassword.Contains('"')) {
    throw 'Passwords cannot contain a double quote.'
}

$content = [IO.File]::ReadAllText($source)
$settings = [ordered]@{
    bIsMultiplay             = 'True'
    GuildPlayerMaxNum        = [string]$Players
    ServerPlayerMaxNum       = [string]$Players
    ServerName               = '"Palpagos After Hours"'
    ServerDescription        = '"Private Palworld server for four friends"'
    AdminPassword            = '"' + $AdminPassword + '"'
    ServerPassword           = '"' + $ServerPassword + '"'
    RCONEnabled              = 'True'
    RCONPort                 = '25575'
    RESTAPIEnabled           = 'True'
    RESTAPIPort              = '8212'
    bShowPlayerList          = 'True'
    bIsUseBackupSaveData     = 'True'
    LogFormatType            = 'Json'
}

foreach ($entry in $settings.GetEnumerator()) {
    $pattern = '(^|,)' + [regex]::Escape($entry.Key) + '=[^,\r\n\)]*'
    $matches = [regex]::Matches(
        $content,
        $pattern,
        [Text.RegularExpressions.RegexOptions]::Multiline
    )
    if ($matches.Count -ne 1) {
        throw "Expected exactly one $($entry.Key) setting, found $($matches.Count)."
    }
    $replacement = '${1}' + $entry.Key + '=' + $entry.Value
    $content = [regex]::Replace(
        $content,
        $pattern,
        $replacement,
        [Text.RegularExpressions.RegexOptions]::Multiline
    )
}

[IO.Directory]::CreateDirectory($configDir) | Out-Null
[IO.File]::WriteAllText(
    $destination,
    $content,
    [Text.UTF8Encoding]::new($false)
)

Write-Host "Configured $destination"
Write-Host "Players: $Players | REST: 8212 | RCON: 25575 | Native backups: enabled"
