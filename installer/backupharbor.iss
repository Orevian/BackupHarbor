; BackupHarbor Windows installer (Inno Setup 6).
;
; Build:  iscc /DMyAppVersion=0.1.0 /DSourceDir=<dist folder> /DOutputDir=<dir> backupharbor.iss
; (scripts\build.ps1 does this for you.)
;
; Design notes
;  - Installs into Program Files (per-machine) or, if the user declines elevation, into the user profile.
;  - Ships the unmodified official restic.exe (BSD 2-Clause, see licenses\) next to the application and never downloads
;    anything at install time: a missing WebView2 Runtime is
;    explained, not silently fixed. The command-line tool works without it.
;  - The optional PATH entry is recorded; uninstall removes only an entry this installer added.
;  - Uninstall keeps configuration and reports unless the user explicitly agrees to delete them.

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif
#ifndef SourceDir
  #error SourceDir must point to the dist folder (BackupHarbor.exe, bharbor.exe, ...)
#endif
#ifndef OutputDir
  #define OutputDir "."
#endif

#define MyAppName "BackupHarbor"
#define MyAppExe "BackupHarbor.exe"
#define MyCliExe "bharbor.exe"

[Setup]
AppId={{2B7D4C19-6E53-4F0A-8A41-93C5E0D7B6F2}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher=BackupHarbor contributors
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#MyAppExe}
UninstallDisplayName={#MyAppName}
SetupIconFile={#SourceDir}\icon.ico
LicenseFile={#SourceDir}\LICENSE
OutputDir={#OutputDir}
OutputBaseFilename=BackupHarbor-{#MyAppVersion}-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog
ChangesEnvironment=yes
CloseApplications=yes
VersionInfoVersion={#MyAppVersion}
VersionInfoProductName={#MyAppName}
VersionInfoDescription={#MyAppName} setup
MinVersion=10.0

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "turkish"; MessagesFile: "compiler:Languages\Turkish.isl"

[CustomMessages]
english.TaskAddPath=Add the bharbor command to the PATH
english.TaskAddPathGroup=Command line:
english.WebView2Missing=The Microsoft Edge WebView2 Runtime is not installed.%n%nThe BackupHarbor desktop application needs it and will show an error until it is installed (https://developer.microsoft.com/microsoft-edge/webview2/). BackupHarbor does not download it for you.%n%nThe command-line tool bharbor works without it.%n%nContinue with the installation?
english.RemoveDataPrompt=Also delete your BackupHarbor configuration, report history and logs?%n%nChoose No to keep them (recommended). Your backup repositories are never touched.
english.OpenApp=Start BackupHarbor
turkish.TaskAddPath=bharbor komutunu PATH'e ekle
turkish.TaskAddPathGroup=Komut satırı:
turkish.WebView2Missing=Microsoft Edge WebView2 Çalışma Zamanı kurulu değil.%n%nBackupHarbor masaüstü uygulaması buna ihtiyaç duyar ve kurulana kadar hata gösterir (https://developer.microsoft.com/microsoft-edge/webview2/). BackupHarbor bunu sizin için indirmez.%n%nKomut satırı aracı bharbor bu bileşen olmadan çalışır.%n%nKuruluma devam edilsin mi?
turkish.RemoveDataPrompt=BackupHarbor yapılandırmanız, rapor geçmişiniz ve günlükleriniz de silinsin mi?%n%nSaklamak için Hayır'ı seçin (önerilir). Yedek depolarınıza asla dokunulmaz.
turkish.OpenApp=BackupHarbor'Ä± başlat

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "addtopath"; Description: "{cm:TaskAddPath}"; GroupDescription: "{cm:TaskAddPathGroup}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: recursesubdirs createallsubdirs ignoreversion; Excludes: "*.pdb"

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExe}"; IconFilename: "{app}\icon.ico"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExe}"; IconFilename: "{app}\icon.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExe}"; Description: "{cm:OpenApp}"; Flags: nowait postinstall skipifsilent

[Code]
const
  WebView2Guid = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}';
  MarkerKey = 'Software\BackupHarbor\Installer';

function PathRoot: Integer;
begin
  if IsAdminInstallMode then
    Result := HKEY_LOCAL_MACHINE
  else
    Result := HKEY_CURRENT_USER;
end;

function PathSubKey: String;
begin
  if IsAdminInstallMode then
    Result := 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
  else
    Result := 'Environment';
end;

function PathContains(const Paths, Dir: String): Boolean;
begin
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Paths) + ';') > 0;
end;

// Adds Dir to PATH unless it is already there. Existing entries are preserved verbatim.
// Ownership is recorded only when this installer actually added the entry.
procedure AddToPath(const Dir: String);
var
  Paths: String;
begin
  if not RegQueryStringValue(PathRoot, PathSubKey, 'Path', Paths) then
    Paths := '';
  if PathContains(Paths, Dir) then
    Exit;
  if (Paths <> '') and (Paths[Length(Paths)] <> ';') then
    Paths := Paths + ';';
  Paths := Paths + Dir;
  if RegWriteExpandStringValue(PathRoot, PathSubKey, 'Path', Paths) then
    RegWriteStringValue(PathRoot, MarkerKey, 'PathEntry', Dir);
end;

// Removes only the entry this installer recorded; foreign entries are left alone.
procedure RemoveFromPath;
var
  Paths, Dir, Wrapped: String;
  P: Integer;
begin
  if not RegQueryStringValue(PathRoot, MarkerKey, 'PathEntry', Dir) then
    Exit;
  if RegQueryStringValue(PathRoot, PathSubKey, 'Path', Paths) then
  begin
    Wrapped := ';' + Paths + ';';
    P := Pos(';' + Uppercase(Dir) + ';', Uppercase(Wrapped));
    if P > 0 then
    begin
      Delete(Wrapped, P, Length(Dir) + 1);
      // Strip the artificial leading/trailing separators again.
      if (Wrapped <> '') and (Wrapped[1] = ';') then Delete(Wrapped, 1, 1);
      if (Wrapped <> '') and (Wrapped[Length(Wrapped)] = ';') then Delete(Wrapped, Length(Wrapped), 1);
      RegWriteExpandStringValue(PathRoot, PathSubKey, 'Path', Wrapped);
    end;
  end;
  RegDeleteValue(PathRoot, MarkerKey, 'PathEntry');
  RegDeleteKeyIfEmpty(PathRoot, MarkerKey);
  RegDeleteKeyIfEmpty(PathRoot, 'Software\BackupHarbor');
end;

function WebView2Installed: Boolean;
var
  V: String;
begin
  Result := False;
  if RegQueryStringValue(HKLM32, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\' + WebView2Guid, 'pv', V) then
    Result := (V <> '') and (V <> '0.0.0.0');
  if (not Result) and RegQueryStringValue(HKLM64, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\' + WebView2Guid, 'pv', V) then
    Result := (V <> '') and (V <> '0.0.0.0');
  if (not Result) and RegQueryStringValue(HKCU, 'Software\Microsoft\EdgeUpdate\Clients\' + WebView2Guid, 'pv', V) then
    Result := (V <> '') and (V <> '0.0.0.0');
end;

function InitializeSetup: Boolean;
begin
  Result := True;
  if not WebView2Installed then
    Result := SuppressibleMsgBox(CustomMessage('WebView2Missing'), mbInformation, MB_OKCANCEL, IDOK) = IDOK;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addtopath') then
    AddToPath(ExpandConstant('{app}'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    // Remove the scheduled task BackupHarbor created (the command only touches its own task).
    Exec(ExpandConstant('{app}\bharbor.exe'), 'schedule remove --quiet --no-color', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromPath;
    if (not UninstallSilent) and
       (MsgBox(CustomMessage('RemoveDataPrompt'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES) then
    begin
      DelTree(ExpandConstant('{userappdata}\BackupHarbor'), True, True, True);
      DelTree(ExpandConstant('{localappdata}\BackupHarbor'), True, True, True);
    end;
  end;
end;
