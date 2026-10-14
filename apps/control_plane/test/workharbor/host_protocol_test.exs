# SPDX-License-Identifier: EUPL-1.2
defmodule WorkHarbor.HostProtocolTest do
  use ExUnit.Case, async: true
  alias WorkHarbor.HostProtocol, as: P
  alias WorkHarbor.HostProtocol.{Peer, Snapshot, Context}
  defp id(n), do: "00000000-0000-0000-0000-" <> String.pad_leading(to_string(n), 12, "0")

  defp vectors do
    raw = File.read!(Path.expand("../../../../contracts/host-protocol/vectors.json", __DIR__))

    assert Base.encode16(:crypto.hash(:sha256, raw), case: :lower) ==
             "dedc629e6989fdfaaad8053fe14a0680b692495fdfff88eb1f4e5a29c534d66d"

    Jason.decode!(raw)
  end

  defp config,
    do: %{
      "repository_identity" => "git@example.test:team/workspace.git",
      "commit" => String.duplicate("a", 40),
      "submodules" => []
    }

  defp command do
    %{
      "schema_version" => 1,
      "kind" => "command",
      "command_id" => id(5),
      "owner_id" => id(1),
      "host_id" => id(2),
      "workspace_id" => id(3),
      "operation_id" => id(4),
      "idempotency_key" => "create:1",
      "request_sha256" => vectors()["request_sha256"],
      "intent_version" => 1,
      "request" => %{
        "schema_version" => 1,
        "workspace_id" => id(3),
        "action" => "create",
        "parameters" => %{"host_id" => id(2), "config" => config()}
      },
      "bindings" => %{"target" => nil, "candidate" => %{"runtime_id" => id(6), "generation" => 1}},
      "execution_config" => nil,
      "approval_ref" => id(7)
    }
  end

  defp context do
    %Context{
      peer: %Peer{kind: :control_plane, owner_id: id(1), host_id: id(2)},
      snapshot: %Snapshot{
        command: command(),
        request_bytes: vectors()["request_bytes"],
        request_hash: vectors()["request_sha256"],
        native_bindings: %{"target" => nil, "candidate" => %{"id" => id(6), "generation" => 1}},
        current_intent: 1,
        status: "queued",
        runtimes: %{id(6) => %{provider_id: nil, effective_config: nil}}
      }
    }
  end

  defp result,
    do:
      command()
      |> Map.drop(["request", "execution_config", "approval_ref"])
      |> Map.put("kind", "result")
      |> Map.put("outcome", %{"status" => "succeeded", "code" => "ok"})

  defp observation, do: %{"runtime_id" => id(6), "generation" => 1, "observed_state" => "running"}

  defp host(c),
    do: %{c | peer: %{c.peer | kind: :host}, snapshot: %{c.snapshot | status: "executing"}}

  test "independent shared raw vectors preserve codes and dispositions" do
    for v <- vectors()["cases"] do
      case P.decode(v["raw"], context()) do
        {:ok, g} ->
          assert v["code"] == "ok", v["id"]
          assert g.disposition == v["disposition"], v["id"]

        {:error, code} ->
          assert Atom.to_string(code) == v["code"], v["id"]
      end
    end
  end

  test "independent canonical anchors separate request hash and command identity" do
    assert {:ok, g} = P.validate(command(), context())
    assert g.canonical == vectors()["command_canonical"]

    assert Base.encode16(:crypto.hash(:sha256, g.canonical), case: :lower) ==
             vectors()["command_sha256"]

    refute g.canonical == vectors()["request_bytes"]
    assert g.native_bindings["candidate"] == %{"id" => id(6), "generation" => 1}
  end

  test "native types exact counters and improper lists reject" do
    for bad <- [nil, true, 1, 1.0, [], {:tuple}, self(), %URI{}],
        do: assert(match?({:error, _}, P.validate(bad, context())))

    for bad <- [true, 1.0, "1", 0, 9_223_372_036_854_775_808] do
      assert {:error, :invalid_message} =
               P.validate(Map.put(command(), "intent_version", bad), context())
    end

    bad = put_in(command(), ["request", "parameters", "config", "submodules"], [%{} | :bad_tail])
    assert {:error, :invalid_message} = P.validate(bad, context())
  end

  test "trusted peer and missing expectation reject before lookup disclosure" do
    bad = context()

    bad = %{
      bad
      | peer: %{bad.peer | owner_id: id(9)},
        snapshot: %{bad.snapshot | command: %{"secret" => "private-canary"}}
    }

    assert {:error, :unauthorized_scope} = P.validate(command(), bad)

    for peer <- [
          nil,
          %Peer{kind: :owner, owner_id: id(1), host_id: id(2)},
          %Peer{kind: :control_plane, owner_id: id(9), host_id: id(2)}
        ] do
      assert {:error, :unauthorized_scope} =
               P.validate(command(), %Context{peer: peer, snapshot: nil})
    end

    assert {:error, :unauthorized_scope} = P.validate(command(), %Context{peer: context().peer})
  end

  test "pending replay and executing command never permit another effect" do
    c = context()
    c = %{c | snapshot: %{c.snapshot | command_bytes: vectors()["command_canonical"]}}
    assert {:ok, %{disposition: "pending_lookup"}} = P.validate(command(), c)

    assert {:ok, %{disposition: "reconciliation_required"}} =
             P.validate(command(), %{c | snapshot: %{c.snapshot | status: "executing"}})
  end

  test "completed historical command replay ignores newer intent" do
    c = context()

    c = %{
      c
      | snapshot: %{
          c.snapshot
          | status: "succeeded",
            current_intent: 2,
            command_bytes: vectors()["command_canonical"]
        }
    }

    assert {:ok, %{disposition: "completed_lookup"}} = P.validate(command(), c)
  end

  test "changed replay and same runtime stale intent reject" do
    c = context()

    assert {:error, :identity_conflict} =
             P.validate(command(), %{c | snapshot: %{c.snapshot | command_bytes: "changed"}})

    assert {:error, :stale_intent} =
             P.validate(command(), %{c | snapshot: %{c.snapshot | current_intent: 2}})
  end

  test "observation omission and optional defaults preserve native nil" do
    c = host(context())
    m = Map.put(result(), "observation", observation())
    assert {:ok, g} = P.validate(m, c)

    assert g.native_result["observation"] ==
             Map.merge(observation(), %{
               "provider_instance_id" => nil,
               "effective_config" => nil,
               "deleted" => false
             })

    assert {:ok, %{native_result: %{"observation" => nil}}} = P.validate(result(), c)
    assert {:error, :invalid_message} = P.validate(Map.put(result(), "observation", nil), c)
  end

  test "malformed observation values never become null" do
    for bad <- [
          false,
          true,
          0,
          "config",
          [],
          %{},
          %{"secret" => "private-canary"},
          %{config() | "submodules" => [%{} | :bad_tail]}
        ] do
      m = Map.put(result(), "observation", Map.put(observation(), "effective_config", bad))
      assert {:error, :invalid_message} = P.validate(m, host(context()))
    end

    for {field, bad} <- [{"provider_instance_id", false}, {"deleted", nil}, {"generation", 1.0}] do
      assert {:error, :invalid_message} =
               P.validate(
                 Map.put(result(), "observation", Map.put(observation(), field, bad)),
                 host(context())
               )
    end
  end

  test "result status and immutable terminal replay are guarded" do
    c = context()
    c = %{c | peer: %{c.peer | kind: :host}}
    assert {:error, :invalid_status} = P.validate(result(), c)
    c = host(c)
    assert {:ok, g} = P.validate(result(), c)

    c = %{
      c
      | snapshot: %{
          c.snapshot
          | status: "succeeded",
            current_intent: 2,
            terminal_result: g.native_result
        }
    }

    assert {:ok, %{disposition: "completed_lookup"}} = P.validate(result(), c)
    m = Map.put(result(), "outcome", %{"status" => "failed", "code" => "operation_failed"})
    assert {:error, :identity_conflict} = P.validate(m, c)
  end

  test "runtime scope provider config and removal observations are guarded" do
    for variant <- [:runtime, :provider, :config, :deleted] do
      c = host(context())
      o = observation()

      {c, o} =
        case variant do
          :runtime ->
            {c, Map.put(o, "runtime_id", id(9))}

          :provider ->
            {%{
               c
               | snapshot: %{
                   c.snapshot
                   | runtimes: %{id(6) => %{provider_id: "known", effective_config: nil}}
                 }
             }, Map.put(o, "provider_instance_id", "changed")}

          :config ->
            {c,
             Map.put(o, "effective_config", %{config() | "commit" => String.duplicate("b", 40)})}

          :deleted ->
            {c, Map.put(o, "deleted", true)}
        end

      assert match?({:error, _}, P.validate(Map.put(result(), "observation", o), c))
    end
  end

  test "result closed shape excludes command only payload fields" do
    for field <- ["request", "execution_config", "approval_ref"],
        do:
          assert(
            {:error, :invalid_message} ==
              P.validate(Map.put(result(), field, nil), host(context()))
          )
  end

  test "native canonical budget aborts before emission and redacts canaries" do
    for text <- [String.duplicate("x", 1_048_576), String.duplicate("\\", 524_288)] do
      m =
        put_in(
          command(),
          ["request", "parameters", "config", "repository_identity"],
          "git@example.test:" <> text
        )

      assert {:error, :transport_refused} = P.validate(m, context())
    end

    assert {:error, :invalid_message} =
             P.validate(Map.put(command(), "secret", "private-canary"), context())
  end

  test "raw size wins invalid Unicode and native depth is bounded" do
    assert {:error, :input_limit} =
             P.decode(<<255>> <> String.duplicate(" ", 1_048_576), context())

    deep = Enum.reduce(1..129, nil, fn _, v -> [v] end)
    assert {:error, :invalid_message} = P.validate(deep, context())
  end

  test "provider sentinel remains untouched for rejection and lookup" do
    for v <- vectors()["cases"], v["code"] != "ok" do
      case P.decode(v["raw"], context()) do
        {:ok, %{disposition: "new_eligible"}} -> send(self(), :provider)
        _ -> :ok
      end
    end

    sentinel = fn value, c ->
      case P.validate(value, c) do
        {:ok, %{disposition: "new_eligible"}} -> send(self(), :provider)
        _ -> :ok
      end
    end

    sentinel.(Map.put(command(), "argv", ["sh"]), context())
    c = context()

    sentinel.(command(), %{
      c
      | snapshot: %{c.snapshot | command_bytes: vectors()["command_canonical"]}
    })

    refute_received :provider
  end

  test "every lifecycle branch and new intent on existing runtime validate" do
    for a <- vectors()["actions"] do
      m = Jason.decode!(a["command_canonical"])
      c = context()

      c = %{
        c
        | snapshot: %{
            c.snapshot
            | command: m,
              request_bytes: a["request_bytes"],
              request_hash: a["request_sha256"],
              native_bindings: a["native_bindings"],
              current_intent: 2,
              runtimes: %{
                id(6) => %{provider_id: nil, effective_config: config()},
                id(8) => %{provider_id: nil, effective_config: nil}
              }
          }
      }

      assert {:ok, %{disposition: "new_eligible"}} = P.decode(a["command_canonical"], c)

      r =
        m
        |> Map.drop(["request", "execution_config", "approval_ref"])
        |> Map.put("kind", "result")
        |> Map.put("outcome", %{"status" => "succeeded", "code" => "ok"})

      o = %{
        "runtime_id" => id(6),
        "generation" => 1,
        "observed_state" => "stopped",
        "effective_config" => config()
      }

      o =
        case a["action"] do
          "start" ->
            Map.put(o, "observed_state", "running")

          "rebuild" ->
            %{o | "runtime_id" => id(8), "generation" => 2, "observed_state" => "running"}

          "remove_runtime" ->
            Map.put(o, "deleted", true)

          _ ->
            o
        end

      assert {:ok, _} = P.validate(Map.put(r, "observation", o), host(c))
    end
  end

  test "canonical exact byte budget and maximum counter are independent" do
    for text <- [
          String.duplicate("x", 1_048_576 - 8),
          String.duplicate("é", div(1_048_576 - 8, 2))
        ] do
      assert {:error, :invalid_message} = P.validate(%{"x" => text}, context())
      assert {:error, :transport_refused} = P.validate(%{"x" => text <> "x"}, context())
    end

    c = context()
    m = Map.put(command(), "intent_version", 9_223_372_036_854_775_807)

    assert {:ok, _} =
             P.validate(m, %{
               c
               | snapshot: %{c.snapshot | command: m, current_intent: 9_223_372_036_854_775_807}
             })
  end

  test "normalized result budget includes optional default overhead" do
    o = Map.put(observation(), "provider_instance_id", "")
    m = Map.put(result(), "observation", o)

    normalized =
      Map.put(m, "observation", Map.merge(o, %{"effective_config" => nil, "deleted" => false}))

    fill = 1_048_576 - byte_size(Jason.encode!(normalized))
    c = host(context())
    exact = put_in(m, ["observation", "provider_instance_id"], String.duplicate("x", fill))
    assert {:ok, g} = P.decode(Jason.encode!(exact), c)
    assert byte_size(g.canonical) == 1_048_576
    over = put_in(m, ["observation", "provider_instance_id"], String.duplicate("x", fill + 1))
    raw = Jason.encode!(over)
    assert byte_size(raw) <= 1_048_576
    assert {:error, :transport_refused} = P.decode(raw, c)
    assert {:error, :transport_refused} = P.validate(over, c)
  end

  test "wire observation effective config is already normalized" do
    for cfg <- [
          Map.delete(config(), "submodules"),
          %{
            config()
            | "submodules" => [
                %{"path" => "z", "commit" => String.duplicate("a", 40)},
                %{"path" => "a", "commit" => String.duplicate("b", 40)}
              ]
          }
        ] do
      m = Map.put(result(), "observation", Map.put(observation(), "effective_config", cfg))
      assert {:error, :invalid_message} = P.validate(m, host(context()))
    end
  end

  test "workspace scope matches separately authorized expectation" do
    m = command() |> Map.put("workspace_id", id(9)) |> put_in(["request", "workspace_id"], id(9))
    assert {:error, :unauthorized_scope} = P.validate(m, context())
  end
end
