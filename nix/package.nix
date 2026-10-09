# The brinketask server with the web client embedded (D-74), built from
# this repository the way `make build` does: the client first, then the
# static binary with the build tag webui, which embeds web/dist.
#
# The two hashes pin the downloaded dependencies. They change whenever
# go.sum or web/package-lock.json do; CLAUDE.md says how to update them,
# and CI fails if they are stale.
{
  lib,
  buildGo127Module,
  buildNpmPackage,
  nodejs_24,
  version,
}:

let
  web = buildNpmPackage {
    pname = "brinketask-web";
    inherit version;
    src = ../web;
    nodejs = nodejs_24;
    npmDepsHash = "sha256-/PvJ+wcNspDTsfeDzmjtZavqQuaZyh9RsMxJzPl2qLI=";
    # `npm run build` type-checks and writes the client to dist/.
    installPhase = ''
      runHook preInstall
      cp -r dist $out
      runHook postInstall
    '';
  };
in
buildGo127Module {
  pname = "brinketask";
  inherit version;
  src = ../.;
  vendorHash = "sha256-ooHDMhagIItWRp5S/N1owkChifyw31MafziHAV0NpXI=";

  subPackages = [ "cmd/brinketask" ];
  tags = [ "webui" ];
  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
  ];

  # go:embed needs the built client inside the source tree.
  preBuild = ''
    cp -r ${web} web/dist
  '';

  # The Go tests need PostgreSQL through a container engine, which the
  # Nix build sandbox does not have. CI runs them (make test); the NixOS
  # test in nix/test.nix exercises the built package.
  doCheck = false;

  passthru = { inherit web; };

  meta = {
    description = "Self-hosted tasks and reminders";
    homepage = "https://github.com/brusapa/brinketask";
    license = lib.licenses.mit;
    mainProgram = "brinketask";
    platforms = lib.platforms.linux;
  };
}
