# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.ConfigTrust.Approval do
  @moduledoc false
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  @timestamps_opts [type: :utc_datetime_usec]
  schema "revision_trust_approvals" do
    field :owner_id, :binary_id
    field :descriptor, :map, redact: true
    field :descriptor_bytes, :binary, redact: true
    field :descriptor_sha256, :string
    field :approved_at, :integer
    timestamps()
  end
end

defmodule WorkHarbor.ConfigTrust do
  @moduledoc "Native exact-revision approval; no HTTP, dispatch or provider effects."
  alias WorkHarbor.{Accounts, Repo, Hosts}
  alias WorkHarbor.Accounts.Principal
  alias WorkHarbor.Foundation.{Request, Canonical, AuditEvent}
  alias __MODULE__.Approval

  def approve(%Principal{} = principal, descriptor) do
    with :ok <- Accounts.authorize(principal, :config_approve),
         {:ok, normalized, bytes, digest} <- normalize(descriptor) do
      result =
        transaction(fn ->
          authorize!(principal, :config_approve)

          case Repo.get_by(Approval, [owner_id: principal.owner_id, descriptor_sha256: digest],
                 log: false
               ) do
            nil ->
              approval =
                Repo.insert!(
                  %Approval{
                    owner_id: principal.owner_id,
                    descriptor: normalized,
                    descriptor_bytes: bytes,
                    descriptor_sha256: digest,
                    approved_at: Accounts.now()
                  },
                  log: false
                )

              Repo.insert!(
                %AuditEvent{
                  owner_id: principal.owner_id,
                  event: "revision_approved",
                  action: "config_approve",
                  channel: channel(principal),
                  outcome: "recorded",
                  code: "ok",
                  metadata: %{"approval_id" => approval.id}
                },
                log: false
              )

              {:ok, approval}

            approval ->
              if approval.descriptor_bytes == bytes and approval.descriptor == normalized,
                do: {:ok, approval},
                else: Repo.rollback(:trust_conflict)
          end
        end)

      with {:error, {:denied, action}} <- result, do: Accounts.deny(principal, action)
    end
  end

  def approve(_, _), do: {:error, :forbidden}

  def lookup(%Principal{} = principal, reference, descriptor) do
    with :ok <- Accounts.authorize(principal, :config_read) do
      lookup_owner(principal.owner_id, reference, descriptor)
    end
  end

  def lookup(_, _, _), do: {:error, :forbidden}

  @doc "Matches retained exact-revision trust for authorized artifact reads only."
  def artifact_approval(%Principal{} = principal, descriptor) do
    with :ok <- Accounts.authorize(principal, :artifact_projection_read),
         {:ok, normalized, bytes, digest} <- normalize(descriptor) do
      case Repo.get_by(Approval, [owner_id: principal.owner_id, descriptor_sha256: digest],
             log: false
           ) do
        %Approval{descriptor: ^normalized, descriptor_bytes: ^bytes} = approval -> {:ok, approval}
        _ -> {:error, :trust_required}
      end
    else
      {:error, :forbidden} -> {:error, :forbidden}
      _ -> {:error, :trust_required}
    end
  end

  def artifact_approval(_, _), do: {:error, :forbidden}

  def lookup_for_host(%Hosts.Peer{} = peer, reference, descriptor) do
    if Hosts.current_peer?(peer),
      do: lookup_owner(peer.owner_id, reference, descriptor),
      else: {:error, :forbidden}
  end

  def lookup_for_host(_, _, _), do: {:error, :forbidden}

  defp lookup_owner(owner, reference, descriptor) do
    with true <- Request.uuid?(reference),
         {:ok, normalized, bytes, digest} <- normalize(descriptor),
         %Approval{} = row <- Repo.get_by(Approval, [id: reference, owner_id: owner], log: false),
         true <-
           row.descriptor_sha256 == digest and row.descriptor_bytes == bytes and
             row.descriptor == normalized do
      {:ok, row}
    else
      _ -> {:error, :trust_required}
    end
  end

  defp normalize(descriptor) do
    with {:ok, normalized} <- Request.normalize_config(descriptor),
         {:ok, bytes} <- Canonical.bytes(normalized) do
      {:ok, normalized, bytes, Base.encode16(:crypto.hash(:sha256, bytes), case: :lower)}
    else
      _ -> {:error, :invalid_configuration}
    end
  end

  defp authorize!(principal, action) do
    if Accounts.authorize(principal, action) != :ok, do: Repo.rollback({:denied, action})
  end

  defp channel(%Principal{kind: "owner_browser"}), do: "browser"
  defp channel(_), do: "cli"

  defp transaction(fun) do
    if Repo.in_transaction?() do
      {:error, :nested_transaction}
    else
      case Repo.transaction(fun, mode: :immediate) do
        {:ok, value} -> value
        {:error, code} -> {:error, code}
      end
    end
  rescue
    _ in [Exqlite.Error, DBConnection.ConnectionError, Ecto.ConstraintError] ->
      {:error, :persistence_failed}
  end
end
