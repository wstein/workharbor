# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.SpecKit do
  @moduledoc "Authorized workspace artifact admission; host artifact transport is unavailable."
  import Ecto.Query
  alias WorkHarbor.{Accounts, ConfigTrust, Repo, StrictJSON}
  alias WorkHarbor.Accounts.Principal
  alias WorkHarbor.Foundation.{Request, Workspace}

  def decode_request(raw) when is_binary(raw) and byte_size(raw) <= 256 do
    case StrictJSON.decode_object(raw) do
      {:ok, request} -> decode_request(request)
      _ -> {:error, :invalid_request}
    end
  end

  def decode_request(%{"workspace_id" => id} = request) when map_size(request) == 1 do
    if Request.uuid?(id), do: {:ok, id}, else: {:error, :invalid_request}
  end

  def decode_request(_), do: {:error, :invalid_request}

  def decode_query("workspace_id=" <> id) do
    decode_request(%{"workspace_id" => id})
  end

  def decode_query(_), do: {:error, :invalid_request}

  def project(%Principal{} = principal, workspace_id) do
    with :ok <- Accounts.authorize(principal, :artifact_projection_read),
         {:ok, ^workspace_id} <- decode_request(%{"workspace_id" => workspace_id}) do
      repo = Repo.get_dynamic_repo()
      deadline = System.monotonic_time(:millisecond) + 1000

      task =
        Task.async(fn ->
          Repo.put_dynamic_repo(repo)

          try do
            project_workspace(principal, workspace_id)
          rescue
            _ -> {:error, :unavailable}
          catch
            :exit, _ -> {:error, :unavailable}
          end
        end)

      case Task.yield(task, max(0, deadline - System.monotonic_time(:millisecond))) ||
             Task.shutdown(task, :brutal_kill) do
        {:ok, result} -> result
        _ -> {:error, :unavailable}
      end
    end
  end

  def project(_, _), do: {:error, :forbidden}

  defp project_workspace(principal, workspace_id) do
    # Check ownership without selecting source fields. A missing row and a
    # foreign row take the same audited path before any source/trust read.
    owned =
      Repo.one(
        from(w in Workspace,
          where: w.id == ^workspace_id and w.owner_id == ^principal.owner_id,
          select: w.id,
          limit: 1
        ),
        log: false
      )

    if owned, do: project_owned(principal, workspace_id), else: not_found(principal)
  end

  defp project_owned(principal, id) do
    workspace = Repo.get_by(Workspace, [id: id, owner_id: principal.owner_id], log: false)

    case workspace do
      nil ->
        not_found(principal)

      row ->
        case ConfigTrust.artifact_approval(principal, row.requested_config) do
          {:ok, approval} ->
            {:ok,
             %{
               availability: :unavailable,
               reason: :host_read_unavailable,
               workspace_id: id,
               repository_identity: approval.descriptor["repository_identity"],
               commit: approval.descriptor["commit"],
               approval_id: approval.id,
               observed_at: Accounts.now()
             }}

          {:error, :forbidden} ->
            {:error, :forbidden}

          _ ->
            {:ok,
             %{
               availability: :unavailable,
               reason: :trust_required,
               workspace_id: id,
               observed_at: Accounts.now()
             }}
        end
    end
  end

  defp not_found(principal) do
    Accounts.deny(principal, :artifact_projection_read)
    {:error, :not_found}
  end
end
