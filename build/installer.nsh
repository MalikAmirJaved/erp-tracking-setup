; ======================================================
; ERP Monitoring – Custom NSIS Installer (Electron-builder Compatible)
; ======================================================

!include "LogicLib.nsh"
!include "WinMessages.nsh"

; ======================================================
; INSTALL MACRO
; ======================================================
!macro customInstall
  DetailPrint "Custom install started"

  ; ----------------------------
  ; Register URI Protocol
  ; ----------------------------
  DeleteRegKey HKCU "Software\Classes\erpmonitoring"
  WriteRegStr HKCU "Software\Classes\erpmonitoring" "" "URL:ERP Monitoring Protocol"
  WriteRegStr HKCU "Software\Classes\erpmonitoring" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\erpmonitoring\shell\open\command" "" '"$INSTDIR\${APP_EXECUTABLE_FILENAME}" "%1"'

  ; ----------------------------
  ; Extract ERPMonitoringDecoder.zip
  ; ----------------------------
  DetailPrint "Extracting ERPMonitoringDecoder.zip..."
  ${If} ${FileExists} "$INSTDIR\resources\ERPMonitoringDecoder.zip"
    ExecWait '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -Command "Expand-Archive -Force \"$INSTDIR\\resources\\ERPMonitoringDecoder.zip\" \"$INSTDIR\""' $0
    ${If} $0 == 0
      DetailPrint "Extraction successful"
      Delete "$INSTDIR\resources\ERPMonitoringDecoder.zip"
    ${Else}
      DetailPrint "Extraction failed with code $0"
    ${EndIf}
  ${Else}
    DetailPrint "ERPMonitoringDecoder.zip not found"
  ${EndIf}

  ; ----------------------------
  ; Add ERPMonitoringDecoder\bin to PATH (safe version)
  ; ----------------------------
  ReadRegStr $0 HKCU "Environment" "Path"
  StrCpy $1 "$INSTDIR\ERPMonitoringDecoder\bin"

  ${If} $0 == ""
    WriteRegExpandStr HKCU "Environment" "Path" "$1"
  ${Else}
    ; Loop to check if $1 already exists in PATH
    StrCpy $2 $0
  pathloop:
    StrCpy $3 $2 1024 ";"           ; get first segment (limit length to avoid issues)
    StrLen $4 $3
    IntOp $4 $4 + 1                 ; +1 for the semicolon
    StrCpy $2 $2 "" $4              ; remove processed part
    StrCmp $3 "" pathdone
    StrCmp $3 $1 pathskip           ; already exists → skip adding
    Goto pathloop
  pathskip:
    Goto pathdone                   ; we can break early
  pathdone:
    ${If} $3 != $1
      WriteRegExpandStr HKCU "Environment" "Path" "$0;$1"
    ${EndIf}
  ${EndIf}

  ; Notify Windows about environment change
  System::Call 'user32::SendMessageTimeoutW(i 0xffff, i ${WM_SETTINGCHANGE}, i 0, w "Environment", i 0, i 5000, *i .r0)'

  DetailPrint "Custom install completed"
!macroend


; ======================================================
; UNINSTALL MACRO
; ======================================================
!macro customUnInstall
  DetailPrint "Custom uninstall started"

  ; ----------------------------
  ; Remove URI Protocol
  ; ----------------------------
  DeleteRegKey HKCU "Software\Classes\erpmonitoring"

  ; ----------------------------
  ; Remove ERPMonitoringDecoder\bin from PATH
  ; ----------------------------
  ReadRegStr $0 HKCU "Environment" "Path"
  StrCpy $1 "$INSTDIR\ERPMonitoringDecoder\bin"

  ${If} $0 != ""
    StrCpy $2 ""
    StrCpy $R0 $0
  unpathloop:
    StrCpy $3 $R0 1024 ";"           ; get segment
    StrLen $4 $3
    IntOp $4 $4 + 1
    StrCpy $R0 $R0 "" $4
    StrCmp $3 "" unpathdone
    StrCmp $3 $1 unskip              ; skip the one we want to remove
    ${If} $2 == ""
      StrCpy $2 $3
    ${Else}
      StrCpy $2 "$2;$3"
    ${EndIf}
  unskip:
    Goto unpathloop
  unpathdone:
    WriteRegExpandStr HKCU "Environment" "Path" "$2"
  ${EndIf}

  ; Notify Windows about environment change
  System::Call 'user32::SendMessageTimeoutW(i 0xffff, i ${WM_SETTINGCHANGE}, i 0, w "Environment", i 0, i 5000, *i .r0)'

  DetailPrint "Custom uninstall completed"
!macroend