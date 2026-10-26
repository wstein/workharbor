# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Hosts.Peer do
  @moduledoc "Internal certificate-registry scope. Transport must prove private-key possession."
  @enforce_keys [:owner_id, :host_id, :identity_id, :certificate, :expires_at, :issuer_expires_at]
  defstruct @enforce_keys
end

defmodule WorkHarbor.Hosts do
  @moduledoc "Atomic one-time enrollment and retained scoped client identities."
  alias WorkHarbor.{Repo, Accounts}
  alias WorkHarbor.Foundation.{Host, AuditEvent, Request}
  alias WorkHarbor.Hosts.{Issuer, Peer}

  def issue_enrollment(principal, host_id) do
    with_authorization(principal, :host_enrollment_issue, fn ->
      transaction(fn ->
        authorize!(principal, :host_enrollment_issue)

        host =
          if is_nil(host_id) do
            host = Repo.insert!(%Host{owner_id: principal.owner_id})

            audit(host.owner_id, host.id, "host_recorded", "host_metadata", "recorded",
              channel: channel(principal)
            )

            host
          else
            scoped_host!(principal.owner_id, host_id)
          end

        sql(
          "INSERT INTO host_security(host_id,owner_id) VALUES(?,?) ON CONFLICT(host_id) DO NOTHING",
          [host.id, host.owner_id]
        )

        not_revoked!(host.id, :host_revoked)
        code = "whe1_" <> Base.url_encode64(:crypto.strong_rand_bytes(32), padding: false)
        now = Accounts.now()

        sql(
          "UPDATE host_enrollments SET revoked_at=? WHERE host_id=? AND consumed_at IS NULL AND revoked_at IS NULL",
          [now, host.id]
        )

        sql(
          "INSERT INTO host_enrollments(id,host_id,owner_id,code_hash,issued_at,expires_at) VALUES(?,?,?,?,?,?)",
          [Ecto.UUID.generate(), host.id, host.owner_id, digest(code), now, now + 300]
        )

        audit(host.owner_id, host.id, "host_enrollment_issued", "host_enrollment_issue", "issued",
          channel: channel(principal)
        )

        %{host_id: host.id, owner_id: host.owner_id, code: code, expires_at: now + 300}
      end)
    end)
  end

  def redeem(code, csr) do
    with true <- code?(code), {:ok, issuer} <- issuer() do
      transaction(fn ->
        enrollment = one("SELECT * FROM host_enrollments WHERE code_hash=?", [digest(code)])
        now = Accounts.now()

        if is_nil(enrollment) or enrollment["issued_at"] > now or enrollment["expires_at"] <= now or
             not is_nil(enrollment["consumed_at"]) or not is_nil(enrollment["revoked_at"]),
           do: Repo.rollback(:invalid_enrollment)

        not_revoked!(enrollment["host_id"], :invalid_enrollment)

        {:ok, cert} =
          or_rollback(Issuer.issue(issuer, csr, enrollment["host_id"], :enrollment, now))

        sql("UPDATE host_enrollments SET consumed_at=? WHERE id=? AND consumed_at IS NULL", [
          now,
          enrollment["id"]
        ])

        sql(
          "UPDATE host_identities SET status='retired' WHERE host_id=? AND status IN ('active','pending')",
          [enrollment["host_id"]]
        )

        identity =
          insert_identity(
            enrollment["owner_id"],
            enrollment["host_id"],
            cert,
            csr,
            nil,
            "active",
            now
          )

        sql("UPDATE hosts SET identity_fingerprint=? WHERE id=?", [
          identity["fingerprint"],
          identity["host_id"]
        ])

        audit(
          identity["owner_id"],
          identity["host_id"],
          "host_enrolled",
          "host_enrollment",
          "issued"
        )

        response(identity, issuer)
      end)
    else
      false -> {:error, :invalid_enrollment}
      error -> error
    end
  end

  # {:error, :unauthorized_host} is a definitive refusal; {:error, :unavailable} is a
  # transient server fault (issuer or database) that callers must not report as one.
  def resolve_identity(cert, mode)
      when is_binary(cert) and byte_size(cert) <= 4096 and mode in [:active, :activation] do
    with {:issuer, {:ok, issuers}} <- {:issuer, trusted_issuers()},
         identity when is_map(identity) <-
           one("SELECT * FROM host_identities WHERE fingerprint=?", [fingerprint(cert)]),
         true <-
           identity["certificate"] == cert and allowed_status?(identity, mode) and
             not_revoked?(identity["host_id"]),
         issuer when is_map(issuer) <-
           Enum.find(
             issuers,
             &Issuer.valid_leaf?(cert, &1, identity["host_id"], Accounts.now())
           ) do
      {:ok,
       %Peer{
         owner_id: identity["owner_id"],
         host_id: identity["host_id"],
         identity_id: identity["id"],
         certificate: cert,
         expires_at: identity["expires_at"],
         issuer_expires_at: issuer.expires_at
       }}
    else
      {:issuer, _} -> {:error, :unavailable}
      _ -> {:error, :unauthorized_host}
    end
  rescue
    _ in [Exqlite.Error, DBConnection.ConnectionError] -> {:error, :unavailable}
  end

  def resolve_identity(_, _), do: {:error, :unauthorized_host}

  def current_peer?(%Peer{} = peer, mode \\ :active) do
    case resolve_identity(peer.certificate, mode) do
      {:ok, current} -> current === peer
      _ -> false
    end
  end

  def renew(%Peer{} = peer, csr) do
    with {:ok, issuer} <- issuer() do
      transaction(fn ->
        if not current_peer?(peer), do: Repo.rollback(:unauthorized_host)
        {:ok, _} = or_rollback(Issuer.csr_public(csr, peer.host_id))

        pending =
          one("SELECT * FROM host_identities WHERE host_id=? AND status='pending'", [peer.host_id])

        if pending do
          if pending["predecessor_id"] != peer.identity_id or
               pending["csr_hash"] != :crypto.hash(:sha256, csr),
             do: Repo.rollback(:rotation_conflict)

          if pending["expires_at"] <= Accounts.now(), do: Repo.rollback(:unauthorized_host)
          response(pending, issuer)
        else
          {:ok, cert} = or_rollback(Issuer.issue(issuer, csr, peer.host_id, :renewal))

          identity =
            insert_identity(
              peer.owner_id,
              peer.host_id,
              cert,
              csr,
              peer.identity_id,
              "pending",
              Accounts.now()
            )

          audit(peer.owner_id, peer.host_id, "host_rotation_staged", "host_renew", "staged")
          response(identity, issuer)
        end
      end)
    end
  end

  def renew(_, _), do: {:error, :unauthorized_host}

  def activate(%Peer{} = peer) do
    result =
      transaction(fn ->
        case resolve_identity(peer.certificate, :activation) do
          {:ok, current} when current === peer -> :ok
          {:error, :unavailable} -> Repo.rollback(:persistence_failed)
          _ -> Repo.rollback(:unauthorized_host)
        end

        row = one("SELECT * FROM host_identities WHERE id=?", [peer.identity_id])

        if row["status"] == "active" do
          if is_nil(row["predecessor_id"]), do: Repo.rollback(:predecessor_missing)
        else
          old =
            one("SELECT * FROM host_identities WHERE id=? AND host_id=? AND status='active'", [
              row["predecessor_id"],
              peer.host_id
            ])

          if is_nil(old), do: Repo.rollback(:predecessor_missing)
          if old["expires_at"] <= Accounts.now(), do: Repo.rollback(:predecessor_expired)

          sql("UPDATE host_identities SET status='retired' WHERE id=?", [old["id"]])

          sql("UPDATE host_identities SET status='active',activated_at=? WHERE id=?", [
            Accounts.now(),
            row["id"]
          ])

          sql("UPDATE hosts SET identity_fingerprint=? WHERE id=?", [
            row["fingerprint"],
            peer.host_id
          ])

          audit(
            peer.owner_id,
            peer.host_id,
            "host_rotation_activated",
            "host_activate",
            "activated"
          )
        end

        :ok
      end)

    if result == {:ok, :ok},
      do: Phoenix.PubSub.broadcast(WorkHarbor.PubSub, "host: " <> peer.host_id, :identity_changed)

    normalize_ok(result)
  end

  def activate(_), do: {:error, :unauthorized_host}

  def revoke(principal, host_id) do
    with_authorization(principal, :host_revoke, fn ->
      result =
        transaction(fn ->
          authorize!(principal, :host_revoke)
          host = scoped_host!(principal.owner_id, host_id)

          sql(
            "INSERT INTO host_security(host_id,owner_id) VALUES(?,?) ON CONFLICT(host_id) DO NOTHING",
            [host.id, host.owner_id]
          )

          unless revoked?(host.id) do
            now = Accounts.now()
            sql("UPDATE host_security SET revoked_at=? WHERE host_id=?", [now, host.id])

            sql(
              "UPDATE host_enrollments SET revoked_at=? WHERE host_id=? AND revoked_at IS NULL",
              [now, host.id]
            )

            sql(
              "UPDATE host_identities SET status='revoked' WHERE host_id=? AND status != 'revoked'",
              [host.id]
            )

            sql("UPDATE host_connection_slots SET state='unknown' WHERE host_id=?", [host.id])

            audit(host.owner_id, host.id, "host_revoked", "host_revoke", "revoked",
              channel: channel(principal)
            )
          end

          :ok
        end)

      if result == {:ok, :ok},
        do: Phoenix.PubSub.broadcast(WorkHarbor.PubSub, "host: " <> host_id, :identity_changed)

      normalize_ok(result)
    end)
  end

  @doc "Owner-bound, secret-free host metadata with one total one-second read budget."
  def list_status(principal, page \\ 1, size \\ 25) do
    with :ok <- Accounts.authorize(principal, :host_status),
         true <- is_integer(page) and page in 1..1000 and is_integer(size) and size in 1..50 do
      repo = Repo.get_dynamic_repo()
      deadline = System.monotonic_time(:millisecond) + 1000

      task =
        Task.async(fn ->
          try do
            Repo.put_dynamic_repo(repo)

            rows =
              sql(
                "SELECT h.id,h.capabilities,h.last_seen,EXISTS(SELECT 1 FROM host_security s WHERE s.host_id=h.id AND s.owner_id=h.owner_id AND s.revoked_at IS NOT NULL) AS revoked FROM hosts h WHERE h.owner_id=? ORDER BY h.id LIMIT ? OFFSET ?",
                [principal.owner_id, size + 1, (page - 1) * size]
              ).rows

            has_next = length(rows) > size

            rows =
              Enum.map(Enum.take(rows, size), fn [id, capabilities, last_seen, revoked] ->
                connection = if revoked == 1, do: "revoked", else: connection_status(id, deadline)

                %{
                  id: id,
                  capabilities: Jason.decode!(capabilities),
                  last_seen: last_seen,
                  revoked: revoked == 1,
                  connection: connection
                }
              end)

            %{
              rows: Enum.take(rows, size),
              has_next: has_next,
              page: page,
              size: size,
              observed_at: Accounts.now()
            }
          rescue
            _ -> :unavailable
          catch
            :exit, _ -> :unavailable
          end
        end)

      case Task.yield(task, max(0, deadline - System.monotonic_time(:millisecond))) ||
             Task.shutdown(task, :brutal_kill) do
        {:ok, result} when is_map(result) -> {:ok, result}
        _ -> {:error, :unavailable}
      end
    else
      false -> {:error, :invalid_pagination}
      error -> error
    end
  rescue
    _ -> {:error, :unavailable}
  catch
    :exit, _ -> {:error, :unavailable}
  end

  defp connection_status(id, deadline) do
    remaining = deadline - System.monotonic_time(:millisecond)

    if remaining <= 0 do
      "unknown"
    else
      case GenServer.call(WorkHarbor.HostConnections, {:host_state, id}, remaining) do
        {:ok, "connected"} -> "connected"
        _ -> "unknown"
      end
    end
  catch
    :exit, _ -> "unknown"
  end

  def status(principal, host_id) do
    with :ok <- Accounts.authorize(principal, :host_status) do
      transaction(fn ->
        host = scoped_host!(principal.owner_id, host_id)
        %{host_id: host.id, owner_id: host.owner_id, revoked: revoked?(host.id)}
      end)
    end
  end

  # Current issuer first, then earlier unexpired issuers kept after rotation.
  defp trusted_issuers do
    case Application.get_env(:workharbor, :host_identity, [])[:issuer_dir] do
      path when is_binary(path) ->
        case Issuer.load(path) do
          {:ok, _} -> {:ok, WorkHarbor.Hosts.TrustedIssuers.issuers(path)}
          error -> error
        end

      _ ->
        {:error, :issuer_unavailable}
    end
  end

  # The trusted issuer that signed `cert`: after a rotation a still-trusted leaf chains to a
  # previous issuer, whose certificate (not the current one) is what the host must receive.
  def issuer_for(cert) when is_binary(cert) do
    with {:ok, issuers} <- trusted_issuers(),
         issuer when is_map(issuer) <-
           Enum.find(issuers, &Issuer.valid_leaf_profile?(cert, &1, Accounts.now())) do
      {:ok, issuer}
    else
      _ -> {:error, :issuer_unavailable}
    end
  end

  def issuer do
    config = Application.get_env(:workharbor, :host_identity, [])

    case config[:issuer_dir] do
      path when is_binary(path) ->
        with {:ok, _} = loaded <- Issuer.load(path) do
          # Remember every issuer that signs, so rotations without a handshake in between
          # cannot lose one from the trusted set.
          WorkHarbor.Hosts.TrustedIssuers.issuers(path)
          loaded
        end

      _ ->
        {:error, :issuer_unavailable}
    end
  end

  def fingerprint(cert), do: :crypto.hash(:sha256, cert) |> Base.encode16(case: :lower)
  def sql(statement, parameters \\ []), do: Repo.query!(statement, parameters, log: false)

  def one(statement, parameters \\ []) do
    case sql(statement, parameters) do
      %{columns: cols, rows: [row]} -> Map.new(Enum.zip(cols, row))
      _ -> nil
    end
  end

  def transaction(fun) do
    if Repo.in_transaction?(),
      do: {:error, :nested_transaction},
      else: Repo.transaction(fun, mode: :immediate, log: false)
  rescue
    _ in [Exqlite.Error, DBConnection.ConnectionError, Ecto.ConstraintError] ->
      {:error, :persistence_failed}
  end

  def audit(owner, host, event, action, outcome, opts \\ []) do
    Repo.insert!(
      %AuditEvent{
        owner_id: owner,
        host_id: host,
        event: event,
        action: action,
        outcome: outcome,
        channel: opts[:channel] || "internal",
        workspace_id: opts[:workspace_id],
        runtime_id: opts[:runtime_id],
        runtime_generation: opts[:generation],
        intent_version: opts[:intent_version]
      },
      log: false
    )
  end

  defp code?("whe1_" <> encoded) when byte_size(encoded) == 43 do
    case Base.url_decode64(encoded, padding: false) do
      {:ok, bytes} ->
        byte_size(bytes) == 32 and Base.url_encode64(bytes, padding: false) == encoded

      _ ->
        false
    end
  end

  defp code?(_), do: false
  defp digest(code), do: :crypto.hash(:sha256, "WorkHarbor enrollment v1\0" <> code)
  defp allowed_status?(row, :active), do: row["status"] == "active"

  defp allowed_status?(row, :activation),
    do:
      row["status"] == "pending" or
        (row["status"] == "active" and not is_nil(row["predecessor_id"]))

  defp not_revoked?(host),
    do:
      not is_nil(
        one("SELECT host_id FROM host_security WHERE host_id=? AND revoked_at IS NULL", [host])
      )

  defp revoked?(host),
    do:
      not is_nil(
        one("SELECT host_id FROM host_security WHERE host_id=? AND revoked_at IS NOT NULL", [host])
      )

  defp not_revoked!(host, error), do: if(not not_revoked?(host), do: Repo.rollback(error))

  defp scoped_host!(owner, id) do
    if not Request.uuid?(id), do: Repo.rollback(:not_found)
    Repo.get_by(Host, id: id, owner_id: owner) || Repo.rollback(:not_found)
  end

  # The in-transaction check rolls back its own denial audit, so the denial is
  # surfaced as a marker and audited here, after the rollback (authentication.md section 4).
  defp authorize!(p, action),
    do: if(Accounts.authorize(p, action) != :ok, do: Repo.rollback({:denied, action}))

  defp with_authorization(principal, action, fun) do
    with :ok <- Accounts.authorize(principal, action) do
      case fun.() do
        {:error, {:denied, ^action}} -> Accounts.deny(principal, action)
        result -> result
      end
    end
  end

  defp channel(%{kind: "cli_owner"}), do: "cli"
  defp channel(_), do: "browser"
  defp or_rollback({:ok, _} = result), do: result
  defp or_rollback({:error, error}), do: Repo.rollback(error)
  defp normalize_ok({:ok, :ok}), do: :ok
  defp normalize_ok(error), do: error

  defp insert_identity(owner, host, certificate, csr, predecessor, status, now) do
    id = Ecto.UUID.generate()
    times = Issuer.inspect_certificate(certificate)

    sql(
      "INSERT INTO host_identities(id,host_id,owner_id,fingerprint,certificate,csr_hash,predecessor_id,status,issued_at,expires_at,activated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
      [
        id,
        host,
        owner,
        fingerprint(certificate),
        certificate,
        :crypto.hash(:sha256, csr),
        predecessor,
        status,
        times.not_before,
        times.not_after,
        if(status == "active", do: now, else: nil)
      ]
    )

    one("SELECT * FROM host_identities WHERE id=?", [id])
  end

  defp response(row, issuer) do
    if byte_size(row["certificate"]) > 4096 or byte_size(issuer.certificate) > 4096,
      do: Repo.rollback(:identity_response_refused)

    result = %{
      "schema_version" => 1,
      "owner_id" => row["owner_id"],
      "host_id" => row["host_id"],
      "identity_id" => row["id"],
      "certificate" => Base.encode64(row["certificate"]),
      "issuer_certificate" => Base.encode64(issuer.certificate),
      "expires_at" => row["expires_at"]
    }

    if byte_size(Jason.encode!(result)) > 16384, do: Repo.rollback(:identity_response_refused)
    result
  end
end
