# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarborWeb.AgentLogFilterTest do
  use ExUnit.Case, async: false
  alias WorkHarborWeb.{AgentLogFilter, AgentSocket}

  test "agent crash events rebuild all report and metadata to fixed safe values" do
    for label <- [{:gen_server, :terminate}, {:proc_lib, :crash}] do
      event = %{
        level: :error,
        msg:
          {:report,
           %{
             label: label,
             state: "SECRET_SENTINEL",
             last_message: "SECRET_SENTINEL",
             log: "SECRET_SENTINEL",
             reason: "SECRET_SENTINEL",
             client_info: "SECRET_SENTINEL",
             extra: "SECRET_SENTINEL",
             report: ["SECRET_SENTINEL"]
           }},
        meta: %{
          workharbor_agent_transport: true,
          pid: self(),
          time: 123,
          domain: [:otp],
          report_cb: "SECRET_SENTINEL",
          error_logger: "SECRET_SENTINEL",
          crash_reason: "SECRET_SENTINEL",
          extra: "SECRET_SENTINEL"
        },
        extra: "SECRET_SENTINEL"
      }

      assert AgentLogFilter.filter(event, []) == %{
               level: :error,
               msg: {:string, "WorkHarbor agent transport refused"},
               meta: %{pid: self(), time: 123, domain: [:workharbor, :agent_transport]}
             }
    end

    fallback = %{
      level: :error,
      msg:
        {:report,
         %{
           label: {:gen_server, :terminate},
           state:
             {:socket,
              %{
                connection: %Bandit.WebSocket.Connection{
                  websock: AgentSocket,
                  websock_state: "SECRET_SENTINEL"
                }
              }}
         }},
      meta: %{pid: "SECRET_SENTINEL", time: "SECRET_SENTINEL", report_cb: "SECRET_SENTINEL"}
    }

    assert AgentLogFilter.filter(fallback, []) == %{
             level: :error,
             msg: {:string, "WorkHarbor agent transport refused"},
             meta: %{domain: [:workharbor, :agent_transport]}
           }
  end

  test "filter is total and leaves unrelated events unchanged" do
    for event <- [
          nil,
          [],
          %{},
          %{
            msg: {:report, %{label: {:gen_server, :terminate}, state: :other}},
            meta: %{pid: self()}
          },
          %{msg: {:report, %{label: :unrelated}}, meta: %{workharbor_agent_transport: true}},
          %{msg: {:report, %{label: {:proc_lib, :crash}}}, meta: :invalid}
        ] do
      assert AgentLogFilter.filter(event, []) == :ignore
    end
  end

  test "owned filter installation is idempotent and refuses a conflicting owner" do
    id = :workharbor_agent_transport
    previous = List.keyfind(:logger.get_primary_config().filters, id, 0)
    :logger.remove_primary_filter(id)

    on_exit(fn ->
      :logger.remove_primary_filter(id)
      if previous, do: :logger.add_primary_filter(id, elem(previous, 1))
    end)

    assert :ok = AgentLogFilter.install()
    assert :ok = AgentLogFilter.install()
    :logger.remove_primary_filter(id)
    conflicting = {fn event, _ -> event end, []}
    assert :ok = :logger.add_primary_filter(id, conflicting)
    assert {:error, :unsafe_agent_logging} = AgentLogFilter.install()
    assert {^id, ^conflicting} = List.keyfind(:logger.get_primary_config().filters, id, 0)
  end
end
