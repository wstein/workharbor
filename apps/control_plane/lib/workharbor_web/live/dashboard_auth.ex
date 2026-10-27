# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.DashboardAuth do
  @moduledoc "Dashboard-only page admission and bounded session revalidation."
  import Phoenix.LiveView
  alias WorkHarbor.Accounts

  def on_mount(:default, _params, session, socket) do
    with %{"credential" => token} <- session,
         {:ok, principal} <- Accounts.authenticate(token, "dashboard"),
         :ok <- Accounts.authorize(principal, :dashboard_page) do
      state = %{principal: principal, timer: nil, ref: nil, stopped: false}
      socket = put_in(socket.private[:dashboard_auth], state)
      socket = attach_hook(socket, :dashboard_navigation, :handle_params, &check_navigation/3)

      socket =
        if connected?(socket) do
          socket
          |> attach_hook(:dashboard_session, :handle_info, &check_message/2)
          |> schedule()
        else
          socket
        end

      {:cont, socket}
    else
      _ -> {:halt, redirect(socket, to: "/login/dashboard")}
    end
  end

  defp check_navigation(_params, _uri, socket) do
    case authorize(socket) do
      {:ok, socket} -> {:cont, socket}
      {:stop, socket} -> {:halt, socket}
    end
  end

  defp check_message({:dashboard_session, ref}, socket) do
    state = socket.private.dashboard_auth

    cond do
      not is_reference(ref) or state.stopped or state.ref != ref ->
        {:halt, socket}

      true ->
        case authorize(socket) do
          {:ok, socket} ->
            socket =
              if function_exported?(socket.view, :dashboard_refresh, 1),
                do: socket.view.dashboard_refresh(socket),
                else: socket

            if socket.private.dashboard_auth.stopped,
              do: {:halt, socket},
              else: {:halt, schedule(socket)}

          {:stop, socket} ->
            {:halt, socket}
        end
    end
  end

  defp check_message(_message, socket), do: {:cont, socket}

  defp authorize(socket) do
    state = socket.private.dashboard_auth

    if not state.stopped and Accounts.authorize(state.principal, :dashboard_page) == :ok do
      {:ok, socket}
    else
      if state.timer, do: Process.cancel_timer(state.timer)
      state = %{state | stopped: true, principal: nil, timer: nil, ref: nil}
      socket = put_in(socket.private[:dashboard_auth], state)

      socket =
        if function_exported?(socket.view, :dashboard_stop, 1),
          do: socket.view.dashboard_stop(socket),
          else: socket

      {:stop, redirect(socket, to: "/login/dashboard")}
    end
  end

  defp schedule(socket) do
    state = socket.private.dashboard_auth
    ref = make_ref()
    timer = Process.send_after(self(), {:dashboard_session, ref}, 1000)
    put_in(socket.private[:dashboard_auth], %{state | timer: timer, ref: ref})
  end
end
