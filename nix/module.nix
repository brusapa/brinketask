# NixOS module: runs brinketask as a hardened systemd service (D-74).
#
# Settings become the environment variables of SPEC section 10. Secrets
# are never written to the Nix store: they are paths to files that
# systemd hands to the service as credentials (LoadCredential), read
# through the server's *_FILE variables.
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.brinketask;
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    types
    ;

  # "127.0.0.1:8080" -> 8080, ":8080" -> 8080.
  portOf = address: lib.toInt (lib.last (lib.splitString ":" address));

  # Secrets as files: credential name -> path on the host.
  credentials = {
    oidc_client_secret = cfg.oidc.clientSecretFile;
    vapid_private_key = cfg.vapid.privateKeyFile;
  }
  // lib.optionalAttrs (cfg.databaseUrlFile != null) { database_url = cfg.databaseUrlFile; };
in
{
  options.services.brinketask = {
    enable = mkEnableOption "brinketask, self-hosted tasks and reminders";

    package = mkOption {
      type = types.package;
      description = "The brinketask package. The flake's module sets its own package as the default.";
    };

    publicUrl = mkOption {
      type = types.str;
      example = "https://tasks.example.com";
      description = ''
        The origin users open (PUBLIC_URL): https and without a path. The
        OIDC callback is this URL followed by /auth/callback.
      '';
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1:8080";
      description = "Address of the application port (LISTEN_ADDR), for the reverse proxy.";
    };

    metricsListenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1:9090";
      description = "Address of GET /metrics (METRICS_LISTEN_ADDR); an empty string turns metrics off.";
    };

    openFirewall = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Open the application port in the firewall. Usually not wanted: the
        reverse proxy on the same host reaches it.
      '';
    };

    logLevel = mkOption {
      type = types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
      description = "LOG_LEVEL.";
    };

    oidc = {
      issuer = mkOption {
        type = types.str;
        example = "https://id.example.com";
        description = "OIDC_ISSUER: the provider's URL, the same for the server and the browsers.";
      };
      clientId = mkOption {
        type = types.str;
        description = "OIDC_CLIENT_ID.";
      };
      clientSecretFile = mkOption {
        # externalPath refuses Nix store paths, and so a path literal such
        # as ./secret, which Nix would copy into the world-readable store.
        type = types.externalPath;
        example = "/run/secrets/brinketask-oidc-client-secret";
        description = "File holding the OIDC client secret. Not copied to the Nix store.";
      };
    };

    vapid = {
      publicKey = mkOption {
        type = types.str;
        description = ''
          VAPID_PUBLIC_KEY, as `brinketask vapid-keys` prints it. Make the
          pair once and keep it: a new pair stops every device's
          notifications until it is turned on again.
        '';
      };
      privateKeyFile = mkOption {
        type = types.externalPath;
        example = "/run/secrets/brinketask-vapid-private-key";
        description = "File holding VAPID_PRIVATE_KEY (the value only). Not copied to the Nix store.";
      };
      subject = mkOption {
        type = types.str;
        example = "mailto:admin@example.com";
        description = "VAPID_SUBJECT: a mailto: or https: URL where push services can reach you.";
      };
    };

    database = {
      createLocally = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Run PostgreSQL on this host (services.postgresql) with a database
          and a user named brinketask that owns it. The service connects
          through the Unix socket with peer authentication, so there is no
          password.
        '';
      };
    };

    databaseUrlFile = mkOption {
      type = types.nullOr types.externalPath;
      default = null;
      description = ''
        File holding DATABASE_URL, for a database elsewhere. Required when
        database.createLocally is false.
      '';
    };

    settings = mkOption {
      type = types.attrsOf types.str;
      default = { };
      example = {
        SESSION_IDLE_TIMEOUT = "72h";
        REMINDER_MAX_LATENESS = "6h";
      };
      description = ''
        Further environment variables of SPEC section 10, such as
        SESSION_IDLE_TIMEOUT, SESSION_MAX_AGE, SCHEDULER_INTERVAL or
        REMINDER_MAX_LATENESS. Not for secrets: they end up in the Nix store.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.database.createLocally || cfg.databaseUrlFile != null;
        message = "services.brinketask: set databaseUrlFile when database.createLocally is false.";
      }
      {
        # Changing PostgreSQL's major version here could leave an existing
        # cluster unable to start, so the module asks instead of choosing.
        assertion =
          !cfg.database.createLocally || lib.versionAtLeast config.services.postgresql.package.version "18";
        message = ''
          services.brinketask needs PostgreSQL 18 or later: the schema uses uuidv7() (SPEC section 10).
          Set services.postgresql.package = pkgs.postgresql_18; an existing cluster of an older major
          version must be upgraded first (NixOS manual, "PostgreSQL", "Upgrading").
        '';
      }
      {
        assertion = !(cfg.database.createLocally && cfg.databaseUrlFile != null);
        message = "services.brinketask: databaseUrlFile and database.createLocally exclude each other.";
      }
    ];

    services.postgresql = mkIf cfg.database.createLocally {
      enable = true;
      ensureDatabases = [ "brinketask" ];
      ensureUsers = [
        {
          name = "brinketask";
          # The owner may create the unaccent extension the first migration
          # needs (SPEC section 10).
          ensureDBOwnership = true;
        }
      ];
    };

    networking.firewall.allowedTCPPorts = mkIf cfg.openFirewall [ (portOf cfg.listenAddress) ];

    systemd.services.brinketask = {
      description = "brinketask";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      # postgresql.target is reached once the database and user exist.
      requires = lib.optional cfg.database.createLocally "postgresql.target";
      after = [ "network-online.target" ] ++ lib.optional cfg.database.createLocally "postgresql.target";

      environment = {
        PUBLIC_URL = cfg.publicUrl;
        LISTEN_ADDR = cfg.listenAddress;
        METRICS_LISTEN_ADDR = cfg.metricsListenAddress;
        LOG_LEVEL = cfg.logLevel;
        OIDC_ISSUER = cfg.oidc.issuer;
        OIDC_CLIENT_ID = cfg.oidc.clientId;
        VAPID_PUBLIC_KEY = cfg.vapid.publicKey;
        VAPID_SUBJECT = cfg.vapid.subject;
        # %d is the directory where systemd puts the credentials below.
        OIDC_CLIENT_SECRET_FILE = "%d/oidc_client_secret";
        VAPID_PRIVATE_KEY_FILE = "%d/vapid_private_key";
      }
      // (
        if cfg.database.createLocally then
          {
            # The Unix socket, as the user brinketask; peer authentication
            # matches it with the role of the same name.
            DATABASE_URL = "postgresql:///brinketask?host=/run/postgresql&user=brinketask";
          }
        else
          { DATABASE_URL_FILE = "%d/database_url"; }
      )
      // cfg.settings;

      serviceConfig = {
        ExecStart = lib.getExe cfg.package;
        Restart = "on-failure";
        RestartSec = "5s";

        # A throwaway user named brinketask: peer authentication needs the
        # name, and the server keeps no files, so no state directory either.
        DynamicUser = true;
        User = "brinketask";
        Group = "brinketask";
        LoadCredential = lib.mapAttrsToList (name: path: "${name}:${path}") credentials;

        # The equivalent of the container's read-only root and no
        # capabilities (SPEC section 10), and more.
        CapabilityBoundingSet = "";
        AmbientCapabilities = "";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        PrivateUsers = true;
        ProtectHostname = true;
        ProtectClock = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectControlGroups = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        RemoveIPC = true;
        SystemCallArchitectures = "native";
        SystemCallFilter = [
          "@system-service"
          "~@privileged"
          "~@resources"
        ];
        UMask = "0077";
      };
    };
  };
}
