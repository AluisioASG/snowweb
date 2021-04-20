# SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>
#
# SPDX-License-Identifier: MIT

{

  inputs = {
    aasg-nixexprs.url = "git+https://git.sr.ht/~aasg/nixexprs";
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { self, aasg-nixexprs, flake-utils, nixpkgs }:
    let

      exports = {
        overlay = final: prev: {
          "snowweb" = {
            defaultPackage = final.buildGoModule {
              name = "snowweb";

              src = self;
              vendorSha256 = "sha256-ZMDk5etjtpN2lEerumlcXmcTrh/PsgQKdnk8ZBhAReU=";

              subPackages = [ "cmd/snowweb" ];

              meta = with nixpkgs.lib; {
                description = "Static website server for Nix packages";
                homepage = "https://git.sr.ht/~aasg/snowweb";
                license = licenses.agpl3;
                maintainers = with maintainers; [ AluisioASG ];
                platforms = platforms.linux;
              };
            };

            devShell = final.mkShell {
              buildInputs = with final; [ delve go golangci-lint gopls reuse ];
            };
          };
        };

        nixosModule = { config, lib, pkgs, ... }:
          let
            inherit (lib) flip mkEnableOption mkIf mkOption types;
            inherit (aasg-nixexprs.lib) concatMapAttrs updateNew;

            settingsToEnv =
              let
                inherit (lib) concatStringsSep isAttrs isList toUpper;
                inherit (aasg-nixexprs.lib) concatMapAttrs;
                settingToEnvAttrs = prefix: name: value:
                  let envName = toUpper "${prefix}_${name}";
                  in
                  if isAttrs value then
                    settingsToEnvAttrs envName value
                  else if isList value then
                    { ${envName} = concatStringsSep "," value; }
                  else if value == null then
                    { }
                  else
                    { ${envName} = toString value; };
                settingsToEnvAttrs = prefix: settings:
                  concatMapAttrs (settingToEnvAttrs prefix) settings;
              in
                /*settings:*/ settingsToEnvAttrs "snowweb";

            cfg = config.services.snowweb;
          in
          {
            options.services.snowweb = {
              enable = mkEnableOption "the SnowWeb static web server";

              package = mkOption {
                description = "snowweb package to use.";
                type = types.package;
                default = pkgs.snowweb.defaultPackage;
                defaultText = "pkgs.snowweb.defaultPackage";
              };

              nixPackage = mkOption {
                description = "nix package used to build sites.";
                type = types.package;
                default = config.nix.package;
                defaultText = "config.nix.package";
              };

              sites = mkOption {
                description = "Sites to serve through SnowWeb.";
                type = types.attrsOf (types.submodule {
                  freeformType = with types; let t = attrsOf (oneOf [ str (listOf str) t ]); in t;

                  options.installable = mkOption {
                    description = "Nix installable to serve.";
                    type = types.str;
                  };

                  options.tls.acme.ca = mkOption {
                    description = "ACME directory of the certificate authority to request certificates from.";
                    type = types.nullOr types.str;
                    default = config.security.acme.server;
                  };

                  options.tls.acme.email = mkOption {
                    description = "Contact email address with which register for an ACME account.";
                    type = types.nullOr types.str;
                    default = config.security.acme.email;
                  };
                });
                default = { };
              };
            };

            config = mkIf cfg.enable {
              systemd.services = flip concatMapAttrs cfg.sites (siteName: siteCfg: {
                "snowweb@${siteName}" = {
                  description = "SnowWeb web server for ${siteName}";
                  requires = [ "network.target" ];
                  after = [ "network.target" ];
                  wantedBy = [ "multi-user.target" ];
                  environment = updateNew (settingsToEnv siteCfg) {
                    NIX_REMOTE = "daemon";
                    XDG_CACHE_HOME = "/tmp";
                    SNOWWEB_PROFILE = "/run/snowweb/%i/profile";
                    SNOWWEB_TLS_ACME_STORAGE = "/var/lib/snowweb";
                  };
                  path = [ cfg.nixPackage pkgs.gitMinimal ];
                  serviceConfig = {
                    Type = "exec";
                    ExecStart = "${cfg.package}/bin/snowweb \${SNOWWEB_INSTALLABLE}";
                    ExecReload = "${pkgs.coreutils}/bin/kill -HUP $MAINPID";
                    Restart = "on-failure";

                    DynamicUser = true;
                    RuntimeDirectory = "snowweb/%i";
                    StateDirectory = "snowweb";

                    NoNewPrivileges = true;
                    ProtectSystem = "strict";
                    ProtectHome = true;
                    ProtectKernelLogs = true;
                    ProtectKernelModules = true;
                    ProtectKernelTunables = true;
                    ProtectControlGroups = true;
                    PrivateDevices = true;
                    PrivateTmp = true;
                    DevicePolicy = "closed";
                    MemoryDenyWriteExecute = true;
                  };
                };
              });
            };
          };
      };

      outputs = flake-utils.lib.simpleFlake rec {
        inherit self nixpkgs;
        name = "snowweb";
        inherit (exports) overlay;
      };

    in

    exports // outputs;

}
