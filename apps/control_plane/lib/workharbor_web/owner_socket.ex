# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerSocket do
  use Phoenix.LiveView.Socket

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
