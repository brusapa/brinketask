# NixOS test of the module (D-74): a virtual machine runs PostgreSQL and
# the brinketask service as the module sets them up, and checks what
# deploy/smoke-test.sh checks for the image.
#
# Like every other test, it makes its VAPID key pair when it runs, so no
# key is ever committed: the service is started by the test script after
# the keys exist, and the real public key, from an environment file,
# overrides the placeholder of the configuration.
{ module }:
{
  name = "brinketask";

  nodes.machine =
    {
      config,
      lib,
      pkgs,
      ...
    }:
    {
      imports = [ module ];

      services.brinketask = {
        enable = true;
        publicUrl = "http://localhost:8080";
        # Discovery happens at the first login, not at startup, so a
        # provider that is not there does not stop the server.
        oidc = {
          issuer = "http://127.0.0.1:1";
          clientId = "brinketask-test";
          clientSecretFile = "/run/brinketask-test/oidc_client_secret";
        };
        vapid = {
          publicKey = "replaced-by-the-test-script";
          privateKeyFile = "/run/brinketask-test/vapid_private_key";
          subject = "mailto:test@example.com";
        };
      };

      # The module asks for 18 or later rather than choosing it.
      services.postgresql.package = pkgs.postgresql_18;

      # Started by the test script once the keys exist.
      systemd.services.brinketask.wantedBy = lib.mkForce [ ];
      # Variables from EnvironmentFile= override Environment=.
      systemd.services.brinketask.serviceConfig.EnvironmentFile = "/run/brinketask-test/vapid.env";

      environment.systemPackages = [
        config.services.brinketask.package
        pkgs.curl
      ];
    };

  testScript = ''
    machine.wait_for_unit("postgresql.target")

    with subtest("the administrator makes the VAPID key pair (D-68) and the secret files"):
        machine.succeed(
            "mkdir -p /run/brinketask-test",
            "echo client-secret-for-tests > /run/brinketask-test/oidc_client_secret",
            "brinketask vapid-keys > /run/brinketask-test/keys",
            "grep '^VAPID_PUBLIC_KEY=' /run/brinketask-test/keys > /run/brinketask-test/vapid.env",
            "sed -n 's/^VAPID_PRIVATE_KEY=//p' /run/brinketask-test/keys > /run/brinketask-test/vapid_private_key",
            "chmod 600 /run/brinketask-test/*",
        )

    with subtest("the service starts, migrates and answers"):
        machine.succeed("systemctl start brinketask")
        machine.wait_for_unit("brinketask.service")
        machine.wait_for_open_port(8080)
        machine.succeed("curl -sf http://127.0.0.1:8080/healthz")
        status = machine.succeed("curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/me")
        assert status == "401", f"GET /api/v1/me answered {status}"
        # Whole outputs are checked in Python: "curl | grep -q" fails at
        # random when grep stops reading before curl has written everything.
        assert "<html" in machine.succeed("curl -sf http://127.0.0.1:8080/")

    with subtest("the database belongs to brinketask and has unaccent"):
        extensions = machine.succeed("sudo -u postgres psql -tAc 'SELECT extname FROM pg_extension' brinketask")
        assert "unaccent" in extensions.split(), extensions
        owner = machine.succeed(
            "sudo -u postgres psql -tAc \"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'brinketask'\""
        )
        assert owner.strip() == "brinketask", owner

    with subtest("metrics on their own port only (D-72)"):
        machine.wait_for_open_port(9090)
        metrics = machine.succeed("curl -sf http://127.0.0.1:9090/metrics").splitlines()
        assert 'brinketask_http_requests_total{code="401",method="GET",route="GET /api/v1/me"} 1' in metrics
        assert "brinketask_http_requests_total" not in machine.succeed("curl -s http://127.0.0.1:8080/metrics")

    with subtest("secrets stay out of the unit's environment"):
        environment = machine.succeed("systemctl show brinketask -p Environment")
        assert "client-secret-for-tests" not in environment
        assert "VAPID_PRIVATE_KEY=" not in environment
        machine.succeed("systemctl show brinketask -p DynamicUser | grep -q yes")
  '';
}
