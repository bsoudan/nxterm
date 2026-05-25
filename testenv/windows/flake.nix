{
  description = "Generic Windows VM test environment (wfvm-based)";

  inputs = {
    # Pinned to the exact rev our NixOS system config locks (nixos-25.11) so
    # the devShell tools resolve to store paths the host already has cached.
    # Bump with `nix flake update` when the system config moves.
    nixpkgs.url = "github:NixOS/nixpkgs/7e495b747b51f95ae15e74377c5ce1fe69c1765f";
    flake-utils.url = "github:numtide/flake-utils";
    wfvm.url = "git+https://git.m-labs.hk/M-Labs/wfvm";
    # NOTE: deliberately not setting `wfvm.inputs.nixpkgs.follows = "nixpkgs"`.
    # wfvm pins to nixos-23.11 internally and may not build under newer trees.
  };

  outputs = { self, nixpkgs, flake-utils, wfvm, ... }:
    let
      # wfvm only supports x86_64-linux.
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};

      # Use wfvm's own OVMF (built from its pinned nixpkgs) so the firmware
      # is byte-identical between image build and runtime. Using a newer
      # nixpkgs's OVMF caused Windows boot to hang silently.
      ovmf-fd = "${wfvm.lib.utils.OVMF.fd}/FV/OVMF.fd";

      image = import ./image.nix { inherit pkgs wfvm; };
    in {
      packages.${system} = {
        image = image;
        default = image;
      };

      devShells.${system}.default = pkgs.mkShell {
        name = "wintest";

        packages = (with pkgs; [
          virt-viewer
          openssh
          sshpass
          swtpm
          socat
          coreutils
          bash
          jq
        ]) ++ [
          # Use wfvm's qemu (from nixos-23.11) so qemu and OVMF are from
          # the same vintage. 25.11's QEMU 10.x hits a KVM emulation
          # failure (RIP=0xa0000) on Haswell when paired with 23.11 OVMF.
          wfvm.lib.utils.qemu
        ];

        shellHook = ''
          export WINTEST_ROOT="$PWD"
          export WINTEST_OVMF_FD="${ovmf-fd}"
          export PATH="$WINTEST_ROOT/bin:$PATH"

          echo
          echo "entering wintest dev environment"
          echo "  base image: nix build .#image  (first build: 30-60 min)"
          echo "  run:        wintest-start | wintest-deploy | wintest-run | wintest-view | wintest-stop"
          echo
        '';
      };
    };
}
