# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerSocket do
  use Phoenix.LiveView.Socket

  @impl Phoenix.Socket
  defdelegate id(socket), to: Phoenix.LiveView.Socket

  defoverridable handle_in: 2

  @impl Phoenix.Socket.Transport
  def handle_in({payload, opts} = message, {_state, socket} = transport) do
    case socket.serializer.decode!(payload, opts) do
      %Phoenix.Socket.Message{topic: "phoenix", event: "heartbeat"} ->
        super(message, transport)

      %Phoenix.Socket.Message{topic: "lv:" <> id, event: "phx_join", payload: payload}
      when id != "" ->
        if valid_join?(payload), do: super(message, transport), else: {:ok, transport}

      %Phoenix.Socket.Message{topic: "lv:" <> id, event: "phx_leave"} when id != "" ->
        super(message, transport)

      _ ->
        {:ok, transport}
    end
  end

  defp valid_join?(%{"session" => session} = payload) when is_binary(session) do
    Enum.all?(["static", "url", "redirect"], fn key ->
      not Map.has_key?(payload, key) or is_binary(payload[key])
    end) and (not Map.has_key?(payload, "params") or is_map(payload["params"]))
  end

  defp valid_join?(_payload), do: false

  def connect(_params, socket, info) do
    with %{"credential" => token} <- info[:session],
         {:ok, p} <- WorkHarbor.Accounts.authenticate(token, "owner_browser"),
         :ok <- WorkHarbor.Accounts.authorize(p, :owner_page) do
      {:ok, Phoenix.Socket.assign(socket, :principal, p)}
    else
      _ -> :error
    end
  end
end

defmodule WorkHarborWeb.DashboardSocket do
  use Phoenix.LiveView.Socket

  @impl Phoenix.Socket
  defdelegate id(socket), to: Phoenix.LiveView.Socket

  defoverridable handle_in: 2

  @impl Phoenix.Socket.Transport
  defdelegate handle_in(message, transport), to: WorkHarborWeb.OwnerSocket

  def connect(_params, socket, info) do
    with %{"credential" => token} <- info[:session],
         {:ok, p} <- WorkHarbor.Accounts.authenticate(token, "dashboard"),
         :ok <- WorkHarbor.Accounts.authorize(p, :dashboard_page) do
      {:ok, Phoenix.Socket.assign(socket, :principal, p)}
    else
      _ -> :error
    end
  end
end
