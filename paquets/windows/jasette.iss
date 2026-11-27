; Jasette — l'installateur Windows (Inno Setup 6), compilé par
; paquets/empaqueter-windows.sh avec ISCC.exe sous wine :
;   ISCC /DVersion=1.0.1 /DNumero=286 /DArbre=<dossier de l'archive> /DSortie=<dossier> jasette.iss
;
; Installation dans le compte de l'utilisateur, sans droits d'administrateur
; (%LOCALAPPDATA%\Programs\Jasette) : Jasette peut ensuite se mettre à jour
; elle-même (src/maj_windows.rs remplace ses fichiers dans ce dossier).
; Les données (%LOCALAPPDATA%\jasette) restent à la désinstallation.

#ifndef Version
  #define Version "0.0.0"
#endif
#ifndef Numero
  #define Numero "0"
#endif

[Setup]
AppId={{6E3B1C52-8F0A-4D4B-9C61-2A7F5E0D9B13}
AppName=Jasette
AppVersion={#Version}
AppVerName=Jasette {#Version} alpha
VersionInfoVersion={#Version}.{#Numero}
AppPublisher=Jonathan Guernon
AppPublisherURL=https://jonathanguernon.net/jasette/
AppSupportURL=https://jonathanguernon.net/jasette/
AppUpdatesURL=https://jonathanguernon.net/jasette/
AppCopyright=Jonathan Guernon — AGPL-3.0-or-later
DefaultDirName={localappdata}\Programs\Jasette
DisableDirPage=auto
DefaultGroupName=Jasette
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
OutputDir={#Sortie}
OutputBaseFilename=jasette-{#Version}-windows-x86_64-setup
SetupIconFile=..\..\assets\windows\jasette.ico
UninstallDisplayIcon={app}\jasette.exe
UninstallDisplayName=Jasette
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
; Jasette et ses ponts tournent peut-être : le gestionnaire de redémarrage les ferme.
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "fr"; MessagesFile: "compiler:Languages\French.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "bureau"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#Arbre}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
; L'AppUserModelID est celui de jasette.exe (src/windows.rs : AUMID).
Name: "{autoprograms}\Jasette"; Filename: "{app}\jasette.exe"; AppUserModelID: "JonathanGuernon.Jasette"
Name: "{autodesktop}\Jasette"; Filename: "{app}\jasette.exe"; AppUserModelID: "JonathanGuernon.Jasette"; Tasks: bureau

[Run]
Filename: "{app}\jasette.exe"; Description: "{cm:LaunchProgram,Jasette}"; Flags: nowait postinstall skipifsilent

[Registry]
; Retirés à la désinstallation : le démarrage avec Windows (Réglages) et
; l'inscription des notifications (posés par Jasette elle-même).
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueName: "Jasette"; ValueType: none; Flags: uninsdeletevalue dontcreatekey
Root: HKCU; Subkey: "Software\Classes\AppUserModelId\JonathanGuernon.Jasette"; ValueType: none; Flags: uninsdeletekey dontcreatekey

[UninstallDelete]
; Les anciens fichiers mis de côté par une mise à jour dans l'app.
Type: files; Name: "{app}\*.ancien*"
