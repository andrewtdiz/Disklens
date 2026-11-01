Unicode True
!include "MUI2.nsh"

Name "Disklens"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\Disklens"
RequestExecutionLevel user
SetCompressor /SOLID lzma
VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "Disklens Setup"
VIAddVersionKey /LANG=1033 "FileDescription" "Disklens installer"
VIAddVersionKey /LANG=1033 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Disklens contributors"

!define MUI_ABORTWARNING
!define MUI_ICON "..\assets\icon.ico"
!define MUI_UNICON "..\assets\icon.ico"
!define MUI_WELCOMEPAGE_TEXT "Install Disklens for your Windows account.$\r$\n$\r$\nYour saved scans and baseline are preserved. Disklens requests administrator access when launched for fast NTFS scanning.$\r$\n$\r$\nClose an installed copy of Disklens before updating."
!define MUI_FINISHPAGE_RUN "$INSTDIR\Disklens.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open Disklens (requests administrator access)"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Disklens application and CLI" AppSection
  SectionIn RO
  SetShellVarContext current
  SetOutPath "$INSTDIR"
  File "${PAYLOAD}\Disklens.exe"
  SetOutPath "$INSTDIR\cli"
  File "${PAYLOAD}\cli\disklens.exe"
  SetOutPath "$INSTDIR"
  File "..\assets\icon.ico"
  File "cli-path.ps1"
  nsExec::ExecToLog '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\cli-path.ps1" -Directory "$INSTDIR\cli"'
  Pop $0
  StrCmp $0 0 pathReady
  MessageBox MB_OK|MB_ICONEXCLAMATION "Could not add the CLI to your user PATH. Installation stopped."
  Abort
  pathReady:
  System::Call 'user32::SendMessageTimeoutW(p 0xffff, i 0x001A, p 0, w "Environment", i 2, i 5000, *p .r0)'
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\Disklens.lnk" "$INSTDIR\Disklens.exe" "" "$INSTDIR\icon.ico"
  CreateShortcut "$SMPROGRAMS\Disklens CLI.lnk" "$SYSDIR\cmd.exe" '/K set "PATH=$INSTDIR\cli;%PATH%" & disklens --help' "$INSTDIR\icon.ico"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "DisplayName" "Disklens"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "DisplayIcon" "$INSTDIR\Disklens.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens" "NoRepair" 1
SectionEnd

Section "Desktop shortcut" DesktopSection
  SetShellVarContext current
  CreateShortcut "$DESKTOP\Disklens.lnk" "$INSTDIR\Disklens.exe" "" "$INSTDIR\icon.ico"
SectionEnd

!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${AppSection} "Desktop app, command-line tool, and Start menu shortcut."
  !insertmacro MUI_DESCRIPTION_TEXT ${DesktopSection} "Add a shortcut to your desktop."
!insertmacro MUI_FUNCTION_DESCRIPTION_END

Section "Uninstall"
  SetShellVarContext current
  ClearErrors
  Delete "$INSTDIR\Disklens.exe"
  Delete "$INSTDIR\cli\disklens.exe"
  IfErrors 0 filesRemoved
  MessageBox MB_OK|MB_ICONEXCLAMATION "Close Disklens and its CLI, then run the uninstaller again."
  Abort
  filesRemoved:
  nsExec::ExecToLog '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\cli-path.ps1" -Directory "$INSTDIR\cli" -Remove'
  Pop $0
  StrCmp $0 0 pathRemoved
  MessageBox MB_OK|MB_ICONEXCLAMATION "Could not remove the CLI PATH entry. Run the uninstaller again."
  Abort
  pathRemoved:
  System::Call 'user32::SendMessageTimeoutW(p 0xffff, i 0x001A, p 0, w "Environment", i 2, i 5000, *p .r0)'
  Delete "$SMPROGRAMS\Disklens.lnk"
  Delete "$SMPROGRAMS\Disklens CLI.lnk"
  Delete "$DESKTOP\Disklens.lnk"
  Delete "$INSTDIR\icon.ico"
  Delete "$INSTDIR\cli-path.ps1"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR\cli"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\Disklens"
SectionEnd
