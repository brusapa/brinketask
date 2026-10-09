# brinketask as a Nix flake (D-74): the package, an overlay, a NixOS
# module and the checks CI runs. See docs/deployment.md, "NixOS".
{
  description = "brinketask: self-hosted tasks and reminders";

  # The stable release whose Go, Node and PostgreSQL match the versions
  # the project pins (Go 1.27.1, Node 24.21.0, PostgreSQL 18).
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      # forAllSystems f = { x86_64-linux = f pkgs; aarch64-linux = f pkgs; }
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # No releases yet: 0.1.0 plus the commit the package was built from.
      version = "0.1.0+${self.shortRev or self.dirtyShortRev or "unknown"}";
      brinketaskFor = pkgs: pkgs.callPackage ./nix/package.nix { inherit version; };
    in
    {
      packages = forAllSystems (pkgs: rec {
        brinketask = brinketaskFor pkgs;
        default = brinketask;
      });

      overlays.default = final: _prev: { brinketask = brinketaskFor final; };

      # The module, with this flake's package as the default.
      nixosModules.default =
        { lib, pkgs, ... }:
        {
          imports = [ ./nix/module.nix ];
          services.brinketask.package =
            lib.mkDefault
              self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };

      # `nix run github:brusapa/brinketask -- vapid-keys`
      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          meta.description = "The brinketask server; `vapid-keys` prints a VAPID key pair";
          program = nixpkgs.lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
      });

      checks = forAllSystems (pkgs: {
        package = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        nixos = pkgs.testers.runNixOSTest (import ./nix/test.nix { module = self.nixosModules.default; });
      });
    };
}
