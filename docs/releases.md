# Releases and updates

Disklens uses MyGo's native updater. **Help → Check for Updates…** checks manually;
packaged apps also check in the background and offer installation and relaunch.
Development builds do not auto-update. Existing builds without the updater need
one manual installation of an updater-enabled release.

## Publish a version

1. Bump `version` in `mygo.json` and commit the changes.
2. Tag that commit with the matching version, for example:

   ```sh
   git tag v0.1.7
   git push origin main v0.1.7
   ```

3. Wait for the **Release** workflow to finish both platform jobs.
4. Review the draft's installers, archives, and `update-*.json` files, then publish
   it as the latest release. Keep beta releases marked as prereleases.

The workflow builds Apple Silicon, Intel, and universal macOS apps plus Windows
x64. Each macOS variant has its own update feed. Windows updates include the CLI
in `cli/disklens.exe`; add that directory to PATH if needed.
An existing published release is not overwritten by rerunning its tag workflow.

## Update-signing key

`mygo.json` contains the public key. The private key is stored in the repository's
`MYGO_UPDATER_PRIVATE_KEY` Actions secret. The local copy is outside the checkout:

```text
~/Library/Application Support/Disklens/update-keys/mygo-update.key
```

Back it up securely. Do not commit it or generate a replacement for routine
releases: installed apps trust the existing key.

Local builds normally omit update archives. To generate signed archives locally:

```sh
export MYGO_UPDATER_PRIVATE_KEY="$(cat "$HOME/Library/Application Support/Disklens/update-keys/mygo-update.key")"
go run github.com/egoist/mygo/cmd/mygo@v0.7.3 build -skip-dmg -platform darwin/arm64
unset MYGO_UPDATER_PRIVATE_KEY
```

## macOS identity and permissions

Update signatures authenticate downloads; they do not provide a stable macOS
app identity. Release builds require a Developer ID Application certificate. Keep
the same developer identity and bundle identifier across releases so macOS can retain
permissions. Moving from the older ad-hoc releases may require one final grant.

Configure these repository secrets for signing; the Apple account credentials
add notarization:

| Secret | Value |
| --- | --- |
| `MACOS_CERTIFICATE` | Base64-encoded Developer ID Application `.p12`, including its private key |
| `MACOS_CERTIFICATE_PASSWORD` | Password for the `.p12` |
| `MACOS_SIGNING_IDENTITY` | Full `Developer ID Application: Name (TEAMID)` identity |
| `APPLE_ID` | Apple developer account email (notarization) |
| `APPLE_TEAM_ID` | Developer team ID (notarization) |
| `APPLE_APP_PASSWORD` | App-specific password for notarization |

The workflow imports the certificate into a temporary runner keychain and removes
it after the build. Missing signing credentials fail the Mac build. Without
`APPLE_APP_PASSWORD`, releases are Developer ID signed but not notarized. Local
builds remain ad-hoc unless a signing identity is passed with `-sign`.

See [MyGo auto-updates](https://mygo.egoist.dev/docs/updates) for signing,
delta updates, and release manifest details.
