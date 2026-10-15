# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Foundation do
  @moduledoc """
  Owner-scoped persistence only. No authentication, provider calls or effect consumer.
  Mutations require a top-level transaction so returned acceptance is committed.
  Owner context must be authenticated by the later API layer, never by request data.
  """
  import Ecto.Query
  alias WorkHarbor.Repo

  alias WorkHarbor.Foundation.{
    Owner,
    Host,
    Workspace,
    Operation,
    PersistentStorage,
    RuntimeInstance,
    RuntimeBinding,
    AuditEvent,
    Request,
    Canonical
  }

  @channels ["cli", "browser", "internal"]
  @terminal ["succeeded", "failed"]
  @counter_max 9_223_372_036_854_775_807

  def create_owner(display_name) when is_binary(display_name) do
    if String.valid?(display_name) and display_name != "" do
      transaction(fn ->
        owner = Repo.insert!(%Owner{display_name: display_name})

        Repo.insert!(%AuditEvent{
          owner_id: owner.id,
          event: "owner_created",
          action: "owner_metadata",
          channel: "internal",
          outcome: "recorded"
        })

        owner
      end)
    else
      {:error, :invalid_request}
    end
  end

  def create_owner(_), do: {:error, :invalid_request}

  def create_host(owner_id) do
    transaction(fn ->
      owner!(owner_id)
      host = Repo.insert!(%Host{owner_id: owner_id})

      Repo.insert!(%AuditEvent{
        owner_id: owner_id,
        host_id: host.id,
        event: "host_recorded",
        action: "host_metadata",
        channel: "internal",
        outcome: "recorded"
      })

      host
    end)
  end

  def accept(owner_id, key, request, channel) do
    with true <- channel in @channels,
         {:ok, normalized} <- Request.normalize(owner_id, key, request) do
      transaction(fn ->
        owner!(owner_id)
        identity = normalized.request

        existing =
          Repo.get_by(Operation,
            owner_id: owner_id,
            workspace_id: identity["workspace_id"],
            idempotency_key: key
          )

        if existing do
          if existing.request_bytes == normalized.bytes and
               existing.request_hash == normalized.hash,
             do: {:replayed, existing},
             else: Repo.rollback(:idempotency_conflict)
        else
          accept_new!(owner_id, key, normalized, channel)
        end
      end)
    else
      false -> {:error, :invalid_channel}
      error -> error
    end
  end

  def lookup(owner_id, workspace_id, key) do
    with true <- Request.uuid?(owner_id) and Request.uuid?(workspace_id) and Request.key?(key),
         %Operation{} = operation <-
           Repo.get_by(Operation,
             owner_id: owner_id,
             workspace_id: workspace_id,
             idempotency_key: key
           ) do
      {:ok, operation}
    else
      _ -> {:error, :not_found}
    end
  end

  def bindings(owner_id, host_id, operation_id) do
    with true <- Enum.all?([owner_id, host_id, operation_id], &Request.uuid?/1),
         {operation, _workspace} <- scoped_operation(owner_id, host_id, operation_id) do
      bindings!(operation.id)
    else
      _ -> {:error, :not_found}
    end
  end

  @doc "Fenced native storage scope and immutable ORIGINAL seed binding; no provider effects."
  def storage_binding(owner, host, operation, intent, storage) do
    transaction(fn ->
      {op, workspace} = scoped_operation!(owner, host, operation)
      current_intent!(op, workspace, intent)
      storage_binding!(op, workspace, storage)
    end)
  end

  @doc "Record the first completed provider handle without replacing retained storage."
  def observe_storage(owner, host, operation, intent, storage, handle) do
    if is_binary(handle) and String.valid?(handle) and handle != "" and
         not String.contains?(handle, <<0>>) do
      transaction(fn ->
        {op, workspace} = scoped_operation!(owner, host, operation)
        current_intent!(op, workspace, intent)
        if op.status != "executing", do: Repo.rollback(:invalid_transition)
        binding = storage_binding!(op, workspace, storage)

        case binding.provider_handle do
          nil ->
            {count, _} =
              Repo.update_all(
                from(s in PersistentStorage,
                  where: s.id == ^storage and is_nil(s.provider_handle)
                ),
                set: [provider_handle: handle, updated_at: now()]
              )

            if count != 1, do: Repo.rollback(:storage_mismatch)

            audit!(op, workspace, "storage_observed", "internal", "recorded",
              storage_ids: [storage]
            )

            %{binding | provider_handle: handle}

          ^handle ->
            binding

          _ ->
            Repo.rollback(:storage_mismatch)
        end
      end)
    else
      {:error, :invalid_request}
    end
  end

  defp storage_binding!(operation, workspace, id) do
    if operation.action not in ["create", "rebuild"], do: Repo.rollback(:invalid_action)
    if operation.status not in ["queued", "executing"], do: Repo.rollback(:invalid_transition)

    storage =
      if Request.uuid?(id),
        do:
          Repo.get_by(PersistentStorage,
            id: id,
            workspace_id: workspace.id,
            host_id: workspace.host_id
          )

    if is_nil(storage) or not is_nil(storage.deleted_at), do: Repo.rollback(:storage_unavailable)

    original =
      Repo.get_by(Operation,
        id: storage.reservation_operation_id,
        owner_id: operation.owner_id,
        workspace_id: workspace.id,
        action: "create"
      )

    if is_nil(original), do: Repo.rollback(:storage_mismatch)

    %{
      storage_id: storage.id,
      owner_id: operation.owner_id,
      host_id: workspace.host_id,
      workspace_id: workspace.id,
      reservation_operation_id: original.id,
      initial_config: original.execution_config_snapshot,
      provider_handle: storage.provider_handle
    }
  end

  def claim(owner_id, host_id, operation_id, intent_version) do
    transaction(fn ->
      {operation, workspace} = scoped_operation!(owner_id, host_id, operation_id)
      current_intent!(operation, workspace, intent_version)

      case operation.status do
        "queued" ->
          updated = update_operation!(operation, status: "executing")
          audit!(updated, workspace, "operation_claimed", "internal", "executing")
          {:claimed, updated}

        "executing" ->
          {:already_executing, operation}

        _ ->
          Repo.rollback(:invalid_transition)
      end
    end)
  end

  def reserve_candidate(owner_id, host_id, operation_id, intent_version) do
    transaction(fn ->
      {operation, workspace} = scoped_operation!(owner_id, host_id, operation_id)
      current_intent!(operation, workspace, intent_version)
      if operation.status not in ["queued", "executing"], do: Repo.rollback(:invalid_transition)
      if operation.action not in ["create", "rebuild"], do: Repo.rollback(:invalid_action)
      candidate!(operation, workspace)
    end)
  end

  def finish(
        owner_id,
        host_id,
        operation_id,
        intent_version,
        expected_status,
        expected_bindings,
        outcome,
        observation \\ nil
      ) do
    with {:ok, result} <- terminal_result(outcome, observation) do
      transaction(fn ->
        {operation, workspace} = scoped_operation!(owner_id, host_id, operation_id)
        if operation.intent_version !== intent_version, do: Repo.rollback(:stale_intent)
        actual = bindings!(operation.id)
        if actual !== expected_bindings, do: Repo.rollback(:runtime_mismatch)

        if operation.status in @terminal do
          if operation.terminal_result === result,
            do: {:replayed, operation},
            else: Repo.rollback(:result_conflict)
        else
          current_intent!(operation, workspace, intent_version)
          if operation.status != expected_status, do: Repo.rollback(:invalid_transition)
          status = result["outcome"]["status"]

          if operation.status not in ["queued", "executing"] or
               (status == "succeeded" and operation.status != "executing"),
             do: Repo.rollback(:invalid_transition)

          observe_terminal!(operation, actual, result)
          updated = update_operation!(operation, status: status, terminal_result: result)

          audit!(updated, workspace, "operation_terminal", "internal", status,
            code: result["outcome"]["code"],
            observation: result["observation"]
          )

          {:finished, updated}
        end
      end)
    end
  end

  defp accept_new!(owner_id, key, normalized, channel) do
    request = normalized.request
    parameters = request["parameters"]
    action = request["action"]
    workspace = Repo.get(Workspace, request["workspace_id"])
    if workspace && workspace.owner_id != owner_id, do: Repo.rollback(:not_found)

    if workspace &&
         Repo.exists?(
           from o in Operation,
             where: o.workspace_id == ^workspace.id and o.status in ["queued", "executing"]
         ),
       do: Repo.rollback(:workspace_busy)

    if action == "create" do
      if workspace, do: Repo.rollback(:workspace_exists)
      host = Repo.get_by(Host, id: parameters["host_id"], owner_id: owner_id)
      if is_nil(host), do: Repo.rollback(:not_found)
    else
      if is_nil(workspace), do: Repo.rollback(:not_found)
    end

    target =
      if workspace,
        do: Repo.get_by(RuntimeInstance, workspace_id: workspace.id, is_current: true),
        else: nil

    if action == "start" and (is_nil(target) or is_nil(target.effective_config)),
      do: Repo.rollback(:runtime_unavailable)

    if action == "rebuild" and
         not Repo.exists?(
           from s in PersistentStorage,
             where: s.workspace_id == ^workspace.id and is_nil(s.deleted_at)
         ),
       do: Repo.rollback(:storage_unavailable)

    desired = if action in ["stop", "remove_runtime"], do: "stopped", else: "running"
    config = parameters["config"] || (workspace && workspace.requested_config)

    workspace =
      if workspace do
        if workspace.intent_version >= @counter_max, do: Repo.rollback(:counter_exhausted)

        {count, _} =
          Repo.update_all(
            from(w in Workspace,
              where: w.id == ^workspace.id and w.intent_version == ^workspace.intent_version
            ),
            set: [
              intent_version: workspace.intent_version + 1,
              desired_state: desired,
              requested_config: config,
              updated_at: now()
            ]
          )

        if count != 1, do: Repo.rollback(:stale_intent)
        Repo.get!(Workspace, workspace.id)
      else
        Repo.insert!(%Workspace{
          id: request["workspace_id"],
          owner_id: owner_id,
          host_id: parameters["host_id"],
          desired_state: desired,
          intent_version: 1,
          requested_config: config
        })
      end

    snapshot = if action == "start", do: target.effective_config, else: parameters["config"]

    operation =
      Repo.insert!(%Operation{
        owner_id: owner_id,
        workspace_id: workspace.id,
        idempotency_key: key,
        action: action,
        request_version: 1,
        request_bytes: normalized.bytes,
        request_hash: normalized.hash,
        intent_version: workspace.intent_version,
        status: "queued",
        execution_config_snapshot: snapshot
      })

    storage_ids =
      if action == "create" do
        storage =
          Repo.insert!(%PersistentStorage{
            workspace_id: workspace.id,
            host_id: workspace.host_id,
            reservation_operation_id: operation.id
          })

        [storage.id]
      else
        []
      end

    if target, do: bind!(operation, target, "target")
    if action in ["create", "rebuild"], do: candidate!(operation, workspace)

    audit!(operation, workspace, "operation_accepted", channel, "queued",
      storage_ids: storage_ids
    )

    {:accepted, operation}
  end

  defp candidate!(operation, workspace) do
    case Repo.get_by(RuntimeInstance, reservation_operation_id: operation.id) do
      %RuntimeInstance{} = existing ->
        binding =
          Repo.get_by(RuntimeBinding,
            operation_id: operation.id,
            role: "candidate",
            runtime_id: existing.id
          )

        if is_nil(binding), do: Repo.rollback(:runtime_mismatch)
        {:replayed, existing}

      nil ->
        if workspace.last_runtime_generation >= @counter_max,
          do: Repo.rollback(:counter_exhausted)

        generation = workspace.last_runtime_generation + 1

        {count, _} =
          Repo.update_all(
            from(w in Workspace,
              where:
                w.id == ^workspace.id and
                  w.last_runtime_generation == ^workspace.last_runtime_generation
            ),
            set: [last_runtime_generation: generation, updated_at: now()]
          )

        if count != 1, do: Repo.rollback(:stale_intent)

        runtime =
          Repo.insert!(%RuntimeInstance{
            workspace_id: workspace.id,
            host_id: workspace.host_id,
            reservation_operation_id: operation.id,
            generation: generation,
            observed_state: "pending"
          })

        bind!(operation, runtime, "candidate")
        audit!(operation, workspace, "runtime_reserved", "internal", "pending", runtime: runtime)
        {:reserved, runtime}
    end
  end

  defp bind!(operation, runtime, role) do
    Repo.insert!(%RuntimeBinding{
      operation_id: operation.id,
      workspace_id: operation.workspace_id,
      runtime_id: runtime.id,
      role: role
    })
  end

  defp bindings!(operation_id) do
    rows =
      Repo.all(
        from b in RuntimeBinding,
          join: r in RuntimeInstance,
          on: r.id == b.runtime_id,
          where: b.operation_id == ^operation_id,
          select: {b.role, r.id, r.generation}
      )

    Enum.reduce(rows, %{"target" => nil, "candidate" => nil}, fn {role, id, generation}, acc ->
      Map.put(acc, role, %{"id" => id, "generation" => generation})
    end)
  end

  defp terminal_result(outcome, observation) do
    allowed = [
      %{"status" => "succeeded", "code" => "ok"},
      %{"status" => "failed", "code" => "operation_failed"}
    ]

    with true <- outcome in allowed,
         {:ok, observation} <- observation(observation),
         {:ok, _} <- Canonical.bytes(%{"outcome" => outcome, "observation" => observation}) do
      {:ok, %{"outcome" => outcome, "observation" => observation}}
    else
      _ -> {:error, :invalid_result}
    end
  end

  defp observation(nil), do: {:ok, nil}

  defp observation(value) when is_map(value) and not is_struct(value) do
    required = ["runtime_id", "generation", "observed_state"]
    optional = ["provider_instance_id", "effective_config", "deleted"]

    valid =
      Enum.all?(required, &Map.has_key?(value, &1)) and
        Enum.all?(Map.keys(value), &(&1 in (required ++ optional))) and
        Request.uuid?(value["runtime_id"]) and is_integer(value["generation"]) and
        value["generation"] > 0 and
        value["observed_state"] in [
          "pending",
          "provisioning",
          "running",
          "stopping",
          "stopped",
          "failed",
          "unknown"
        ] and
        Map.get(value, "deleted", false) in [true, false] and
        (is_nil(value["provider_instance_id"]) or
           (is_binary(value["provider_instance_id"]) and
              String.valid?(value["provider_instance_id"])))

    with true <- valid,
         {:ok, effective} <- effective_config(value["effective_config"]) do
      {:ok,
       %{"provider_instance_id" => nil, "deleted" => false}
       |> Map.merge(value)
       |> Map.put("effective_config", effective)}
    else
      _ -> {:error, :invalid_result}
    end
  end

  defp observation(_), do: {:error, :invalid_result}

  defp effective_config(nil), do: {:ok, nil}
  defp effective_config(value), do: Request.normalize_config(value)

  defp observe_terminal!(_operation, _bindings, %{"observation" => nil}), do: :ok

  defp observe_terminal!(operation, bindings, result) do
    observed = result["observation"]
    identity = %{"id" => observed["runtime_id"], "generation" => observed["generation"]}
    role = Enum.find_value(bindings, fn {role, binding} -> if binding == identity, do: role end)
    if is_nil(role), do: Repo.rollback(:runtime_mismatch)
    runtime = Repo.get!(RuntimeInstance, identity["id"])
    provider = observed["provider_instance_id"]
    effective = observed["effective_config"]

    if not is_nil(runtime.provider_instance_id) and not is_nil(provider) and
         runtime.provider_instance_id != provider,
       do: Repo.rollback(:runtime_mismatch)

    if role == "target" and not is_nil(effective) and is_nil(runtime.effective_config),
      do: Repo.rollback(:runtime_mismatch)

    if not is_nil(runtime.effective_config) and not is_nil(effective) and
         runtime.effective_config !== effective,
       do: Repo.rollback(:runtime_mismatch)

    if not is_nil(effective) and role == "candidate" and
         effective !== operation.execution_config_snapshot,
       do: Repo.rollback(:runtime_mismatch)

    if operation.action == "start" and not is_nil(effective) and
         effective !== operation.execution_config_snapshot,
       do: Repo.rollback(:runtime_mismatch)

    deleted = observed["deleted"]

    if deleted and
         (operation.action != "remove_runtime" or role != "target" or
            result["outcome"]["status"] != "succeeded" or observed["observed_state"] != "stopped"),
       do: Repo.rollback(:invalid_result)

    changes = [
      observed_state: observed["observed_state"],
      provider_instance_id:
        if(is_nil(provider), do: runtime.provider_instance_id, else: provider),
      effective_config: if(is_nil(effective), do: runtime.effective_config, else: effective)
    ]

    changes = if deleted, do: changes ++ [deleted_at: now(), is_current: false], else: changes
    Repo.update!(Ecto.Changeset.change(runtime, changes))
  end

  defp update_operation!(operation, changes) do
    {count, _} =
      Repo.update_all(
        from(o in Operation, where: o.id == ^operation.id and o.status == ^operation.status),
        set: changes ++ [updated_at: now()]
      )

    if count != 1, do: Repo.rollback(:invalid_transition)
    Repo.get!(Operation, operation.id)
  end

  defp audit!(operation, workspace, event, channel, outcome, options \\ []) do
    runtime = options[:runtime]
    observed = options[:observation]
    metadata = if options[:storage_ids], do: %{"storage_ids" => options[:storage_ids]}, else: %{}

    Repo.insert!(%AuditEvent{
      owner_id: operation.owner_id,
      host_id: workspace.host_id,
      workspace_id: workspace.id,
      operation_id: operation.id,
      event: event,
      action: operation.action,
      intent_version: operation.intent_version,
      channel: channel,
      outcome: outcome,
      code: options[:code],
      runtime_id: (runtime && runtime.id) || (observed && observed["runtime_id"]),
      runtime_generation: (runtime && runtime.generation) || (observed && observed["generation"]),
      metadata: metadata
    })
  end

  defp owner!(id) do
    if not Request.uuid?(id) or is_nil(Repo.get(Owner, id)), do: Repo.rollback(:not_found)
  end

  defp scoped_operation(owner, host, id) do
    Repo.one(
      from o in Operation,
        join: w in Workspace,
        on: w.id == o.workspace_id,
        where:
          o.id == ^id and o.owner_id == ^owner and w.owner_id == ^owner and w.host_id == ^host,
        select: {o, w}
    )
  end

  defp scoped_operation!(owner, host, id) do
    if not Enum.all?([owner, host, id], &Request.uuid?/1), do: Repo.rollback(:not_found)
    scoped_operation(owner, host, id) || Repo.rollback(:not_found)
  end

  defp current_intent!(operation, workspace, expected) do
    if operation.intent_version !== expected or workspace.intent_version !== expected,
      do: Repo.rollback(:stale_intent)
  end

  defp now, do: DateTime.utc_now()

  defp transaction(work) do
    if Repo.in_transaction?() do
      {:error, :nested_transaction}
    else
      Repo.transaction(work, mode: :immediate)
    end
  rescue
    error in [Exqlite.Error, DBConnection.ConnectionError, Ecto.ConstraintError] ->
      # Never return adapter exceptions, SQL or their parameter-bearing diagnostics.
      if String.contains?(Exception.message(error), ["database is locked", "database is busy"]),
        do: {:error, :persistence_busy},
        else: {:error, :persistence_failed}
  end
end
