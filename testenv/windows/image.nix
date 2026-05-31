{ pkgs, wfvm }:

let
  # Disable UAC so subsequent SSH-launched installers inherit a fully
  # elevated admin token. UAC requires a reboot to take effect, which
  # happens naturally between wfvm layers (each layer ends in shutdown).
  disable-uac = {
    name = "disable-uac";
    script = ''
      echo "Disabling UAC..."
      win-exec 'reg add HKLM\Software\Microsoft\Windows\CurrentVersion\Policies\System /v EnableLUA /t REG_DWORD /d 0 /f'
      win-exec 'reg add HKLM\Software\Microsoft\Windows\CurrentVersion\Policies\System /v ConsentPromptBehaviorAdmin /t REG_DWORD /d 0 /f'
    '';
  };

  # Auto-login as wfvm so per-user processes like vdagent.exe (SPICE
  # clipboard, seamless mouse) launch on boot. Built-in wfvm user's
  # password is "1234".
  enable-autologon = {
    name = "enable-autologon";
    script = ''
      echo "Configuring autologon for wfvm user..."
      win-exec 'reg add "HKLM\Software\Microsoft\Windows NT\CurrentVersion\Winlogon" /v DefaultUserName /t REG_SZ /d wfvm /f'
      win-exec 'reg add "HKLM\Software\Microsoft\Windows NT\CurrentVersion\Winlogon" /v DefaultPassword /t REG_SZ /d 1234 /f'
      win-exec 'reg add "HKLM\Software\Microsoft\Windows NT\CurrentVersion\Winlogon" /v AutoAdminLogon /t REG_SZ /d 1 /f'
    '';
  };

  # Install the virtio-serial driver (vioser) directly from the
  # virtio-win.iso via pnputil. This bypasses the virtio-win-guest-tools
  # GUI installer, which hangs on an invisible driver-trust dialog during
  # headless layer builds. pnputil is CLI-only and respects the disabled
  # UAC. Without this driver, no SPICE agent channel can open and
  # spice-vdagent silently exits with code 0.
  vioserial-driver = {
    name = "vioserial-driver";
    script = let
      iso = pkgs.fetchurl {
        name = "virtio-win.iso";
        url = "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso";
        # Placeholder — `nix build .#image` will print the real hash.
        sha256 = "sha256-4UzyuUSSw+kl8AcLp/3+3rIEjJHuqcWlr7MCMqOXYzE=";
      };
    in ''
      ln -s ${iso} ./virtio-win.iso

      cat > install-vioserial.ps1 <<'PSEOF'
      $ErrorActionPreference = "Stop"
      $iso = "C:\Users\wfvm\virtio-win.iso"
      Write-Host "Mounting $iso..."
      Mount-DiskImage -ImagePath $iso | Out-Null
      Start-Sleep -Seconds 2
      $drive = (Get-DiskImage -ImagePath $iso | Get-Volume).DriveLetter
      Write-Host "Mounted at $($drive):"
      $infGlob = "$($drive):\vioserial\w11\amd64\*.inf"
      Write-Host "Installing drivers matching $infGlob"
      & pnputil /add-driver $infGlob /install
      Write-Host "Dismounting..."
      Dismount-DiskImage -ImagePath $iso | Out-Null
      Write-Host "Done."
      PSEOF

      win-put virtio-win.iso .
      win-put install-vioserial.ps1 .
      win-exec 'powershell -ExecutionPolicy Bypass -File install-vioserial.ps1'
    '';
  };

  # spice-guest-tools installs the per-user vdagent.exe and the SYSTEM
  # vdservice. With vioserial already installed (previous layer),
  # vdservice can actually open the SPICE channel. The installer also
  # forgets to set up vdagent autorun and benefits from delayed-auto
  # service start; we fix both below.
  spice-guest-tools = {
    name = "spice-guest-tools";
    script = let
      installer = pkgs.fetchurl {
        name = "spice-guest-tools.exe";
        url = "https://www.spice-space.org/download/windows/spice-guest-tools/spice-guest-tools-latest.exe";
        sha256 = "sha256-tb4HVIArzX9/4Mzbh3+KYiS6E6KvfYTrCHqJs7AjfaI=";
      };
    in ''
      ln -s ${installer} ./spice-guest-tools.exe
      win-put spice-guest-tools.exe .
      echo "Installing SPICE guest tools..."
      win-exec 'start /wait "" .\spice-guest-tools.exe /S'

      # vdservice is registered AUTO_START but can lose a race with
      # vioserial PnP completion on cold boot. Delayed-auto starts it
      # after the rest of boot settles.
      echo "Configuring vdservice for reliable startup..."
      win-exec 'sc config vdservice start= delayed-auto'
      win-exec 'sc failure vdservice reset= 86400 actions= restart/60000/restart/60000/restart/60000'

      # The installer drops vdagent.exe in Program Files but doesn't add
      # an autorun entry, so it never launches at login. Push a .reg file
      # rather than fight cmd.exe quoting for a path with spaces/parens.
      echo "Registering vdagent for autorun..."
      cat > vdagent-run.reg <<'REGEOF'
      Windows Registry Editor Version 5.00

      [HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Run]
      "vdagent"="C:\\Program Files (x86)\\SPICE Guest Tools\\64\\vdagent.exe"
      REGEOF
      win-put vdagent-run.reg .
      win-exec 'reg import vdagent-run.reg'
      echo "SPICE guest tools installed"
    '';
  };
in

# Generic Windows test image. Deliberately contains no project-specific
# software — binaries are deployed at runtime via `wintest-deploy`.
#
# The base wfvm image already installs OpenSSH server and a `wfvm` user
# (password "1234"). Layers below tune the system for headless test use:
# disable the firewall (so the host can reach SSH on the forwarded port),
# disable autosleep/autolock (so the VM stays responsive), and disable
# scheduled defragmentation (which would bloat the qcow2 needlessly).

wfvm.lib.makeWindowsImage {
  # Pin to the locally-added ISO. wfvm's default sha256 is for whatever
  # ISO Microsoft was serving when wfvm was last updated; Microsoft re-spins
  # these silently, so we override with the user's actual file. To use a
  # different ISO, update the sha256 here.
  windowsImage = pkgs.requireFile {
    name = "Win11_25H2_English_x64_v2.iso";
    sha256 = "d141f6030fed50f75e2b03e1eb2e53646c4b21e5386047cb860af5223f102a32";
    message = ''
      Download Win11_25H2_English_x64_v2.iso from
      https://www.microsoft.com/en-us/software-download/windows11/
      and add it to the nix store:
        nix-store --add-fixed sha256 Win11_25H2_English_x64_v2.iso
      If your ISO has a different sha256, update image.nix accordingly.
    '';
  };

  # Generic volume license key (GVLK) for Windows 11 Pro N. Lets installation
  # complete; does NOT activate. Fine for VMs / CI / testing.
  productKey = "MH37W-N47XK-V7XM9-C7227-GCQG9";
  imageSelection = "Windows 11 Pro N";

  installCommands = with wfvm.lib.layers; [
    (collapseLayers [
      disable-firewall
      disable-autosleep
      disable-autolock
      disable-scheduled-defrag
    ])
    disable-uac
    enable-autologon
    vioserial-driver
    spice-guest-tools
  ];
}
