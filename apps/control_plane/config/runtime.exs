# SPDX-License-Identifier: EUPL-1.2
import Config

# No listener, demo-secret fallback, HTTP redirect or forwarded-header trust.
# Explicit activation remains an operator decision outside development checks.
config :workharbor, WorkHarborWeb.Endpoint, server: false, http: false

if config_env() == :prod do
  database = System.get_env("DATABASE_PATH") || raise "DATABASE_PATH is required"
  config :workharbor, WorkHarbor.Repo, database: database, pool_size: 5
end

if System.get_env("PHX_SERVER") == "true" do
  required = fn name ->
    System.get_env(name) || raise "unsafe authentication server configuration"
  end

  origin = required.("WORKHARBOR_ORIGIN")
  uri = URI.parse(origin)
  {:ok, bind} = :inet.parse_address(String.to_charlist(required.("WORKHARBOR_BIND")))

  config :workharbor, :owner_http,
    origin: origin,
    authorities: [{String.downcase(uri.host), uri.port}]

  config :workharbor, WorkHarborWeb.Endpoint,
    server: true,
    http: false,
    debug_errors: false,
    code_reloader: false,
    watchers: [],
    secret_key_base: required.("SECRET_KEY_BASE"),
    url: [scheme: "https", host: uri.host, port: uri.port],
    https: [
      ip: bind,
      port: uri.port,
      certfile: required.("WORKHARBOR_TLS_CERT_FILE"),
      keyfile: required.("WORKHARBOR_TLS_KEY_FILE"),
      # Temporary GHSA-69m6-wcv4-rwcf mitigation; see transport-security contract.
      websocket_options: [max_fragmented_message_size: 65_536],
      http_1_options: [enabled: true],
      http_2_options: [enabled: false]
    ]
end

# The issuer must already exist; initialization is a separate explicit command.
if issuer_dir = System.get_env("WORKHARBOR_ISSUER_DIR") do
  config :workharbor, :host_identity, issuer_dir: issuer_dir
end
