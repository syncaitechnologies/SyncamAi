; SyncCam Box installer (Inno Setup 6). Built by installer\build.ps1:
;   ISCC /DAppVersion=0.1.0 /DBundleDir=<PyInstaller folder> SyncCamBox.iss
;
; Interactive install: asks for the pairing code from the dashboard.
; Unattended install (IT tools): SyncCamSetup.exe /VERYSILENT /CODE=K7Q4XM92
; Silent updates started by the box itself keep the existing pairing.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef BundleDir
  #define BundleDir "..\build\pyinstaller\dist\syncam-box"
#endif

[Setup]
AppId={{6B3D2F7A-4C1E-4E8B-9A57-2D0F8C31B5E4}
AppName=SyncCam Box
AppVersion={#AppVersion}
AppVerName=SyncCam Box {#AppVersion}
AppPublisher=SyncAI Technologies
AppPublisherURL=https://syncam-ai-alert-center.vercel.app
DefaultDirName={autopf}\SyncCam Box
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
OutputBaseFilename=SyncCamSetup-{#AppVersion}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName=SyncCam Box
UninstallDisplayIcon={app}\syncam-box.exe
CloseApplications=no
SetupLogging=yes

[Files]
Source: "{#BundleDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "configure-box.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\SyncCam Box status"; Filename: "http://127.0.0.1:8790/"

[Run]
Filename: "http://127.0.0.1:8790/"; Description: "Open the SyncCam Box status page (ready within a minute)"; Flags: postinstall shellexec skipifsilent nowait

[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\configure-box.ps1"" -Remove"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveSyncCamBoxTask"

[Code]
var
  PairPage: TInputQueryWizardPage;

function BoxHome: String;
begin
  Result := ExpandConstant('{commonappdata}\SyncCam');
end;

function IsPaired: Boolean;
begin
  Result := FileExists(BoxHome + '\syncam-box.vault');
end;

function CleanCode(Value: String): String;
begin
  Result := Uppercase(Trim(Value));
  StringChangeEx(Result, '-', '', True);
  StringChangeEx(Result, ' ', '', True);
end;

function PairingCode: String;
begin
  Result := CleanCode(ExpandConstant('{param:CODE|}'));
  if (Result = '') and (PairPage <> nil) then
    Result := CleanCode(PairPage.Values[0]);
end;

procedure InitializeWizard;
begin
  PairPage := CreateInputQueryPage(wpSelectDir,
    'Pair this computer', 'Connect SyncCam Box to your site',
    'In the SyncCam dashboard open Configuration > Site box and press "Pair a site box". ' +
    'Type the 8-character code it shows. The code works once, for 30 minutes.' + #13#10#13#10 +
    'Install SyncCam Box on a computer that stays on and is on the same network as the cameras or recorder.');
  PairPage.Add('Pairing code (for example K7Q4-XM92):', False);
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (PageID = PairPage.ID) and (IsPaired or (ExpandConstant('{param:CODE|}') <> ''));
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (CurPageID = PairPage.ID) and (Length(CleanCode(PairPage.Values[0])) <> 8) then
  begin
    MsgBox('The pairing code has 8 letters and digits, for example K7Q4-XM92.', mbError, MB_OK);
    Result := False;
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  { Stop a running box so its files can be replaced. }
  Exec(ExpandConstant('{sys}\schtasks.exe'), '/End /TN "SyncCam Box"', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM syncam-box.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Result := '';
end;

function RunHidden(FileName, Params: String): Integer;
begin
  if not Exec(FileName, Params, ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, Result) then
    Result := -1;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  PowerShell, Script, Params, CodeFile, Log: String;
  Code: Integer;
begin
  if CurStep <> ssPostInstall then
    Exit;
  PowerShell := ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe');
  Script := '-NoProfile -ExecutionPolicy Bypass -File "' + ExpandConstant('{app}\configure-box.ps1') + '"';
  Log := BoxHome + '\logs\setup.log';

  { Lock the box folder before the vault is written. A machine-scope DPAPI
    vault in a folder other accounts can read would expose the site key, so
    stop here if the lock fails. }
  if RunHidden(PowerShell, Script + ' -LockOnly') <> 0 then
  begin
    SuppressibleMsgBox('SyncCam Box could not restrict access to ' + BoxHome + ', so it did not pair this computer or store any keys. ' +
      'Run the installer again as Administrator.', mbCriticalError, MB_OK, IDOK);
    Exit;
  end;

  WizardForm.StatusLabel.Caption := 'Downloading the detection model and pairing this computer...';
  Params := 'setup --name "' + GetComputerNameString + '" --log-file "' + Log + '"';
  CodeFile := '';
  if (not IsPaired) and (PairingCode <> '') then
  begin
    { The code goes through a temporary file, not the command line. }
    CodeFile := ExpandConstant('{tmp}\pairing-code.txt');
    SaveStringToFile(CodeFile, PairingCode, False);
    Params := Params + ' --code-file "' + CodeFile + '"';
  end;
  Code := RunHidden(ExpandConstant('{app}\syncam-box.exe'), Params);
  if CodeFile <> '' then
    DeleteFile(CodeFile);
  if Code <> 0 then
    SuppressibleMsgBox('SyncCam Box is installed, but setup did not finish (pairing or model download). ' +
      'The details are in ' + Log + '.' + #13#10#13#10 +
      'If pairing failed, create a new code in the dashboard and run as Administrator:' + #13#10 +
      '"' + ExpandConstant('{app}\syncam-box.exe') + '" pair --code <code>', mbError, MB_OK, IDOK);

  WizardForm.StatusLabel.Caption := 'Starting SyncCam Box...';
  if RunHidden(PowerShell, Script + ' -Exe "' + ExpandConstant('{app}\syncam-box.exe') + '"') <> 0 then
    SuppressibleMsgBox('SyncCam Box could not register its start-up task. Run the installer again as Administrator.', mbError, MB_OK, IDOK);
end;
