!macro customInstall
  DetailPrint "Registering ERPMonitoring URI Handler"

  DeleteRegKey HKCU "Software\Classes\erpmonitoring"

  WriteRegStr HKCU "Software\Classes\erpmonitoring" "" "URL:ERP Monitoring Protocol"
  WriteRegStr HKCU "Software\Classes\erpmonitoring" "URL Protocol" ""

  WriteRegStr HKCU "Software\Classes\erpmonitoring\DefaultIcon" "" "$INSTDIR\${APP_EXECUTABLE_FILENAME},0"

  WriteRegStr HKCU "Software\Classes\erpmonitoring\shell\open\command" "" '"$INSTDIR\${APP_EXECUTABLE_FILENAME}" "%1"'
!macroend

!macro customUnInstall
  DetailPrint "Unregistering ERPMonitoring URI Handler"
  DeleteRegKey HKCU "Software\Classes\erpmonitoring"
!macroend
