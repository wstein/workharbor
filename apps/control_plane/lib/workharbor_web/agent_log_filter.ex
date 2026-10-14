# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AgentLogFilter do
  @moduledoc "Rebuild only agent OTP crash reports before any logger handler receives raw data."
  @id :workharbor_agent_transport
  def install do
    expected = {&__MODULE__.filter/2, []}

    case List.keyfind(:logger.get_primary_config().filters, @id, 0) do
      {@id, ^expected} ->
        :ok

      nil ->
        case :logger.add_primary_filter(@id, expected) do
          :ok ->
            :ok

          _ ->
            if List.keyfind(:logger.get_primary_config().filters, @id, 0) == {@id, expected},
              do: :ok,
              else: {:error, :unsafe_agent_logging}
        end

      _ ->
        {:error, :unsafe_agent_logging}
    end
  rescue
    _ -> {:error, :unsafe_agent_logging}
  end

  def filter(%{msg: {:report, report}, meta: meta}, _) when is_map(report) and is_map(meta) do
    if agent_report?(report, meta) do
      safe = %{domain: [:workharbor, :agent_transport]}
      safe = if is_pid(meta[:pid]), do: Map.put(safe, :pid, meta[:pid]), else: safe
      safe = if is_integer(meta[:time]), do: Map.put(safe, :time, meta[:time]), else: safe
      %{level: :error, msg: {:string, "WorkHarbor agent transport refused"}, meta: safe}
    else
      :ignore
    end
  end

  def filter(_, _), do: :ignore

  defp agent_report?(%{label: label} = report, meta)
       when label in [{:gen_server, :terminate}, {:proc_lib, :crash}],
       do: meta[:workharbor_agent_transport] === true or agent_state?(report)

  defp agent_report?(_, _), do: false

  defp agent_state?(%{
         label: {:gen_server, :terminate},
         state:
           {_, %{connection: %Bandit.WebSocket.Connection{websock: WorkHarborWeb.AgentSocket}}}
       }),
       do: true

  defp agent_state?(_), do: false
end
