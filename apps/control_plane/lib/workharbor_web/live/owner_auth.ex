# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.OwnerAuth do
  @moduledoc "Owner-only page admission and bounded session revalidation."
  import Phoenix.LiveView
  alias WorkHarbor.Accounts

  def on_mount(:default, _params, session, socket) do
    with %{"credential" => token} <- session,
         {:ok, principal} <- Accounts.authenticate(token, "owner_browser"),
         :ok <- Accounts.authorize(principal, :owner_page) do
      state = %{principal: principal, timer: nil, ref: nil, stopped: false}
      socket = put_in(socket.private[:owner_auth], state)
      socket = attach_hook(socket, :owner_navigation, :handle_params, &check_navigation/3)

      socket =
        if connected?(socket) do
          socket
          |> attach_hook(:owner_session, :handle_info, &check_message/2)
          |> schedule()
        else
          socket
        end

      {:cont, socket}
    else
      _ -> {:halt, redirect(socket, to: "/login/owner")}
    end
  end

  defp check_navigation(_params, _uri, socket) do
    case authorize(socket) do
      {:ok, socket} -> {:cont, socket}
      {:stop, socket} -> {:halt, socket}
    end
  end

  defp check_message({:owner_session, ref}, socket) do
    state = socket.private.owner_auth

    cond do
      state.stopped or state.ref != ref ->
        {:halt, socket}

      true ->
        case authorize(socket) do
          {:ok, socket} -> {:halt, schedule(socket)}
          {:stop, socket} -> {:halt, socket}
        end
    end
  end

  defp check_message(_message, socket), do: {:cont, socket}

  defp authorize(socket) do
    state = socket.private.owner_auth

    if not state.stopped and Accounts.authorize(state.principal, :owner_page) == :ok do
      {:ok, socket}
    else
      if state.timer, do: Process.cancel_timer(state.timer)
      state = %{state | stopped: true, principal: nil, timer: nil, ref: nil}
      socket = put_in(socket.private[:owner_auth], state)
      {:stop, redirect(socket, to: "/login/owner")}
    end
  end

  defp schedule(socket) do
    state = socket.private.owner_auth
    ref = make_ref()
    timer = Process.send_after(self(), {:owner_session, ref}, 1000)
    put_in(socket.private[:owner_auth], %{state | timer: timer, ref: ref})
  end
end
