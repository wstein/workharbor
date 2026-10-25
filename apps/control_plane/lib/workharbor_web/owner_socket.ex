# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerSocket do
  use Phoenix.LiveView.Socket

  defoverridable handle_in: 2

  @impl Phoenix.Socket.Transport
  def handle_in({payload, opts} = message, {_state, socket} = transport) do
    case socket.serializer.decode!(payload, opts) do
      %Phoenix.Socket.Message{event: "event"} -> {:ok, transport}
      _ -> super(message, transport)
    end
  end

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
