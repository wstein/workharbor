# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostProtocol.Peer do
  @moduledoc "Caller-supplied verified peer scope, never decoded from wire data."
  defstruct [:kind, :owner_id, :host_id]
end

defmodule WorkHarbor.HostProtocol.Snapshot do
  @moduledoc "Authoritative operation/command expectation; not a durable ledger."
  defstruct [
    :command,
    :request_bytes,
    :request_hash,
    :native_bindings,
    :current_intent,
    :status,
    :command_bytes,
    :terminal_result,
    runtimes: %{}
  ]
end

defmodule WorkHarbor.HostProtocol.Context do
  @moduledoc "Trusted peer and separately supplied operation expectation."
  defstruct [:peer, :snapshot]
end

defmodule WorkHarbor.HostProtocol do
  @moduledoc "Pure closed host protocol validation. No returned disposition permits effects."
  alias WorkHarbor.{StrictJSON}
  alias WorkHarbor.Foundation.{Request, Canonical}
  alias __MODULE__.{Context, Peer, Snapshot}
  @limit 1_048_576
  @max 9_223_372_036_854_775_807
  @integer_limit Integer.pow(10, 4300)
  @common ~w(schema_version kind command_id owner_id host_id workspace_id operation_id idempotency_key request_sha256 intent_version bindings)
  @command @common ++ ~w(request execution_config approval_ref)
  @states ~w(pending provisioning running stopping stopped failed unknown)

  def decode(raw, context) do
    case StrictJSON.decode_object(raw) do
      {:ok, value} -> validate(value, context)
      {:error, :input_limit_exceeded} -> {:error, :input_limit}
      _ -> {:error, :invalid_json}
    end
  end

  def validate(value, context) do
    budget!(value, 0, @limit)
    message = shape!(value)
    budget!(message, 0, @limit)
    {peer, snapshot} = scope!(message, context)
    budget!(snapshot.command, 0, @limit)
    expected = command!(snapshot.command)

    valid!(
      expected["owner_id"] == peer.owner_id and expected["host_id"] == peer.host_id,
      :unauthorized_scope
    )

    valid!(message["workspace_id"] == expected["workspace_id"], :unauthorized_scope)

    for field <- @common -- ["kind"] do
      valid!(message[field] === expected[field], :identity_conflict)
    end

    {:ok, normalized} =
      Request.normalize(peer.owner_id, expected["idempotency_key"], expected["request"])

    valid!(
      normalized.bytes === snapshot.request_bytes and normalized.hash === snapshot.request_hash and
        expected["request_sha256"] === snapshot.request_hash,
      :identity_conflict
    )

    native = native_bindings!(message["bindings"])
    valid!(native === snapshot.native_bindings, :stale_runtime)
    {:ok, canonical} = Canonical.bytes(message)

    {disposition, result} =
      if message["kind"] == "command" do
        valid!(message === expected, :identity_conflict)

        if not is_nil(snapshot.command_bytes),
          do: valid!(snapshot.command_bytes === canonical, :identity_conflict)

        {command_disposition!(snapshot, message["intent_version"]), nil}
      else
        result = %{
          "outcome" => message["outcome"],
          "observation" => Map.get(message, "observation")
        }

        if snapshot.status in ["succeeded", "failed"] do
          valid!(snapshot.terminal_result === result, :identity_conflict)
          {"completed_lookup", result}
        else
          current!(snapshot, message["intent_version"])

          valid!(
            snapshot.status in ["queued", "executing"] and
              (message["outcome"]["status"] != "succeeded" or snapshot.status == "executing"),
            :invalid_status
          )

          observe!(result, native, expected, snapshot)
          {"new_eligible", result}
        end
      end

    {:ok,
     %{
       message: message,
       canonical: canonical,
       native_bindings: native,
       native_result: result,
       disposition: disposition
     }}
  catch
    code
    when code in [
           :invalid_message,
           :unsupported_version,
           :unauthorized_scope,
           :identity_conflict,
           :stale_intent,
           :stale_runtime,
           :invalid_status,
           :transport_refused
         ] ->
      {:error, code}
  end

  defp shape!(v) do
    valid!(is_map(v) and not is_struct(v))

    if is_integer(v["schema_version"]) and v["schema_version"] !== 1,
      do: throw(:unsupported_version)

    valid!(v["schema_version"] === 1)

    case v["kind"] do
      "command" ->
        command!(v)

      "result" ->
        keys!(v, @common ++ ["outcome"], ["observation"])
        common!(v)

        valid!(
          v["outcome"] === %{"status" => "succeeded", "code" => "ok"} or
            v["outcome"] === %{"status" => "failed", "code" => "operation_failed"}
        )

        if Map.has_key?(v, "observation"),
          do: Map.put(v, "observation", observation!(v["observation"])),
          else: v

      _ ->
        throw(:invalid_message)
    end
  end

  defp command!(v) do
    keys!(v, @command)
    valid!(v["schema_version"] === 1 and v["kind"] == "command")
    common!(v)

    case Request.normalize(v["owner_id"], v["idempotency_key"], v["request"]) do
      {:ok, normalized} -> valid!(Map.delete(normalized.request, "owner_id") === v["request"])
      _ -> throw(:invalid_message)
    end

    req = v["request"]
    valid!(req["workspace_id"] == v["workspace_id"])
    action = req["action"]
    b = v["bindings"]

    case action do
      "create" ->
        valid!(
          req["parameters"]["host_id"] == v["host_id"] and is_nil(b["target"]) and
            not is_nil(b["candidate"])
        )

      "rebuild" ->
        valid!(not is_nil(b["candidate"]))

      "start" ->
        valid!(not is_nil(b["target"]) and is_nil(b["candidate"]))

      _ ->
        valid!(is_nil(b["candidate"]))
    end

    if action == "start",
      do: config!(v["execution_config"], true),
      else: valid!(is_nil(v["execution_config"]))

    if action in ["create", "rebuild"],
      do: valid!(Request.uuid?(v["approval_ref"])),
      else: valid!(is_nil(v["approval_ref"]))

    v
  end

  defp common!(v) do
    for field <- ~w(command_id owner_id host_id workspace_id operation_id),
        do: valid!(Request.uuid?(v[field]))

    valid!(Request.key?(v["idempotency_key"]))

    valid!(
      is_binary(v["request_sha256"]) and Regex.match?(~r/\A[0-9a-f]{64}\z/, v["request_sha256"])
    )

    counter!(v["intent_version"])
    native_bindings!(v["bindings"])
  end

  defp native_bindings!(b) do
    keys!(b, ~w(target candidate))

    Map.new(b, fn {role, value} ->
      if is_nil(value) do
        {role, nil}
      else
        keys!(value, ~w(runtime_id generation))
        valid!(Request.uuid?(value["runtime_id"]))
        counter!(value["generation"])
        {role, %{"id" => value["runtime_id"], "generation" => value["generation"]}}
      end
    end)
  end

  defp observation!(o) do
    keys!(
      o,
      ~w(runtime_id generation observed_state),
      ~w(provider_instance_id effective_config deleted)
    )

    valid!(Request.uuid?(o["runtime_id"]) and o["observed_state"] in @states)
    counter!(o["generation"])
    provider = Map.get(o, "provider_instance_id")
    valid!(is_nil(provider) or (is_binary(provider) and String.valid?(provider)))
    deleted = Map.get(o, "deleted", false)
    valid!(deleted === true or deleted === false)

    effective =
      case Map.get(o, "effective_config") do
        nil -> nil
        config -> config!(config, true)
      end

    o
    |> Map.put("provider_instance_id", provider)
    |> Map.put("effective_config", effective)
    |> Map.put("deleted", deleted)
  end

  defp observe!(%{"observation" => nil}, _, _, _), do: :ok

  defp observe!(%{"observation" => o, "outcome" => outcome}, bindings, command, snapshot) do
    identity = %{"id" => o["runtime_id"], "generation" => o["generation"]}
    role = Enum.find_value(bindings, fn {role, value} -> if value === identity, do: role end)
    valid!(not is_nil(role), :stale_runtime)
    runtime = Map.get(snapshot.runtimes, o["runtime_id"])
    valid!(is_map(runtime), :stale_runtime)
    provider = o["provider_instance_id"]
    effective = o["effective_config"]

    if not is_nil(provider) and not is_nil(runtime[:provider_id]),
      do: valid!(provider === runtime[:provider_id], :stale_runtime)

    if not is_nil(effective) do
      if role == "target", do: valid!(not is_nil(runtime[:effective_config]), :stale_runtime)

      if not is_nil(runtime[:effective_config]),
        do: valid!(effective === runtime[:effective_config], :stale_runtime)

      expected =
        if role == "candidate",
          do: command["request"]["parameters"]["config"],
          else: command["execution_config"]

      if role == "candidate" or command["request"]["action"] == "start",
        do: valid!(effective === expected, :stale_runtime)
    end

    if o["deleted"] do
      valid!(
        command["request"]["action"] == "remove_runtime" and role == "target" and
          outcome["status"] == "succeeded" and o["observed_state"] == "stopped"
      )
    end
  end

  defp scope!(m, %Context{peer: %Peer{} = peer, snapshot: %Snapshot{} = snapshot}) do
    kind = if m["kind"] == "command", do: :control_plane, else: :host

    valid!(
      peer.kind == kind and Request.uuid?(peer.owner_id) and Request.uuid?(peer.host_id) and
        peer.owner_id == m["owner_id"] and peer.host_id == m["host_id"],
      :unauthorized_scope
    )

    {peer, snapshot}
  end

  defp scope!(_, _), do: throw(:unauthorized_scope)

  defp command_disposition!(%Snapshot{status: status}, _) when status in ["succeeded", "failed"],
    do: "completed_lookup"

  defp command_disposition!(s, intent) do
    current!(s, intent)

    case s.status do
      "executing" -> "reconciliation_required"
      "queued" -> if is_nil(s.command_bytes), do: "new_eligible", else: "pending_lookup"
      _ -> throw(:invalid_status)
    end
  end

  defp current!(s, intent), do: valid!(s.current_intent === intent, :stale_intent)

  defp config!(v, strict) do
    case Request.normalize_config(v) do
      {:ok, normalized} ->
        if strict, do: valid!(v === normalized)
        normalized

      _ ->
        throw(:invalid_message)
    end
  end

  defp keys!(v, required, optional \\ []) do
    valid!(is_map(v) and not is_struct(v))

    valid!(
      Enum.all?(required, &Map.has_key?(v, &1)) and
        Enum.all?(Map.keys(v), &(&1 in (required ++ optional)))
    )
  end

  defp counter!(v), do: valid!(is_integer(v) and v >= 1 and v <= @max)
  defp valid!(v, code \\ :invalid_message)
  defp valid!(true, _), do: :ok
  defp valid!(_, code), do: throw(code)

  # Count exact canonical size while retaining the generic native profile. Never
  # call the unbounded canonical allocator until this pass has succeeded.
  defp budget!(_, _, left) when left < 0, do: throw(:transport_refused)
  defp budget!(nil, _, left), do: spend!(left, 4)
  defp budget!(true, _, left), do: spend!(left, 4)
  defp budget!(false, _, left), do: spend!(left, 5)

  defp budget!(v, _, left) when is_integer(v) do
    valid!(v > -@integer_limit and v < @integer_limit)
    spend!(left, byte_size(Integer.to_string(v)))
  end

  defp budget!(v, _, left) when is_binary(v) do
    if byte_size(v) > left, do: throw(:transport_refused)
    valid!(String.valid?(v))

    string_budget!(v, spend!(left, 2))
  end

  defp budget!(v, depth, left) when is_list(v) and depth < 128 do
    list_budget!(v, depth + 1, spend!(left, 2), false)
  end

  defp budget!(v, depth, left) when is_map(v) and not is_struct(v) and depth < 128 do
    Enum.reduce(v, {spend!(left, 2), false}, fn {k, value}, {remaining, comma} ->
      valid!(is_binary(k))
      remaining = budget!(k, depth + 1, spend!(remaining, if(comma, do: 2, else: 1)))
      {budget!(value, depth + 1, remaining), true}
    end)
    |> elem(0)
  end

  defp budget!(_, _, _), do: throw(:invalid_message)
  defp list_budget!([], _, left, _), do: left

  defp list_budget!([v | rest], depth, left, comma),
    do:
      list_budget!(rest, depth, budget!(v, depth, spend!(left, if(comma, do: 1, else: 0))), true)

  defp list_budget!(_, _, _, _), do: throw(:invalid_message)

  defp string_budget!(<<>>, left), do: left

  defp string_budget!(<<c::utf8, rest::binary>>, left) do
    n =
      cond do
        c in [34, 92, 8, 12, 10, 13, 9] -> 2
        c < 32 -> 6
        true -> byte_size(<<c::utf8>>)
      end

    string_budget!(rest, spend!(left, n))
  end

  defp spend!(left, n) do
    if left < n, do: throw(:transport_refused), else: left - n
  end
end
