# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.Endpoint do
  use Phoenix.Endpoint, otp_app: :workharbor

  socket "/owner/live", WorkHarborWeb.OwnerSocket,
    websocket: [
      connect_info: [session: {WorkHarborWeb.BrowserSession, :options, [:owner]}],
      check_csrf: true,
      check_origin: {WorkHarborWeb.Security, :socket_origin, []}
    ],
    longpoll: false

  socket "/dashboard/live", WorkHarborWeb.DashboardSocket,
    websocket: [
      connect_info: [session: {WorkHarborWeb.BrowserSession, :options, [:dashboard]}],
      check_csrf: true,
      check_origin: {WorkHarborWeb.Security, :socket_origin, []}
    ],
    longpoll: false

  # Phoenix inserts socket_dispatch before user plugs. Wrap its entry explicitly.
  def call(conn, opts) do
    admission =
      if String.starts_with?(conn.request_path, "/agent"),
        do: WorkHarborWeb.AgentIngress.call(conn, []),
        else: WorkHarborWeb.Security.call(conn, [])

    case admission do
      %{halted: true} = conn -> conn
      conn -> super(conn, opts)
    end
  end

  plug Plug.RequestId
  plug Plug.Telemetry, event_prefix: [:phoenix, :endpoint]
  plug Plug.Static,
    at: "/assets",
    from: {:workharbor, "priv/static/assets"},
    only: ~w(css js),
    gzip: false,
    cache_control_for_etags: "no-store",
    cache_control_for_vsn_requests: "no-store"

  plug WorkHarborWeb.BrowserSession
  plug WorkHarborWeb.Router
end
