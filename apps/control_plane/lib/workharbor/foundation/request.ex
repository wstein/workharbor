# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Foundation.Request do
  @moduledoc "Accepted native lifecycle schema. Raw HTTP decoding is a later boundary."
  alias WorkHarbor.Foundation.{Canonical, RepositoryIdentity}
  @empty_actions ["start", "stop", "remove_runtime"]

  def normalize(owner, key, request) do
    valid!(uuid?(owner))
    valid!(key?(key))
    keys!(request, ["schema_version", "workspace_id", "action"], ["parameters"])
    valid!(request["schema_version"] === 1 and uuid?(request["workspace_id"]))
    action = request["action"]
    parameters = parameters!(action, Map.get(request, "parameters", :missing))

    normalized = %{
      "schema_version" => 1,
      "owner_id" => owner,
      "workspace_id" => request["workspace_id"],
      "action" => action,
      "parameters" => parameters
    }

    {:ok, bytes} = Canonical.bytes(normalized)

    {:ok,
     %{
       request: normalized,
       bytes: bytes,
       hash: Base.encode16(:crypto.hash(:sha256, bytes), case: :lower)
     }}
  catch
    :invalid_request -> {:error, :invalid_request}
  end

  @doc "Normalizes an immutable lexical config descriptor without fetching or trust resolution."
  def normalize_config(value) do
    {:ok, config!(value)}
  catch
    :invalid_request -> {:error, :invalid_request}
  end

  def key?(value) when is_binary(value), do: Regex.match?(~r/\A[A-Za-z0-9._:-]{1,128}\z/, value)
  def key?(_), do: false

  def uuid?(value) when is_binary(value),
    do: Regex.match?(~r/\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/, value)

  def uuid?(_), do: false

  defp parameters!(action, :missing) when action in @empty_actions, do: %{}

  defp parameters!(action, value) when action in @empty_actions do
    keys!(value, [])
    value
  end

  defp parameters!("create", value) do
    keys!(value, ["host_id", "config"])
    valid!(uuid?(value["host_id"]))
    %{"host_id" => value["host_id"], "config" => config!(value["config"])}
  end

  defp parameters!("rebuild", value) do
    keys!(value, ["config"])
    %{"config" => config!(value["config"])}
  end

  defp parameters!(_, _), do: throw(:invalid_request)

  defp config!(value) do
    keys!(value, ["repository_identity", "commit"], ["submodules"])
    valid!(RepositoryIdentity.valid?(value["repository_identity"]) and commit?(value["commit"]))
    entries = Map.get(value, "submodules", [])
    valid!(proper_list?(entries))

    entries =
      Enum.map(entries, fn entry ->
        keys!(entry, ["path", "commit"])
        valid!(path?(entry["path"]) and commit?(entry["commit"]))
        entry
      end)

    valid!(length(Enum.uniq_by(entries, & &1["path"])) == length(entries))

    %{
      "repository_identity" => value["repository_identity"],
      "commit" => value["commit"],
      "submodules" => Enum.sort_by(entries, & &1["path"])
    }
  end

  defp commit?(value) when is_binary(value),
    do: Regex.match?(~r/\A(?:[0-9a-f]{40}|[0-9a-f]{64})\z/, value)

  defp commit?(_), do: false

  defp path?(value) when is_binary(value) do
    String.valid?(value) and value != "" and not String.starts_with?(value, "/") and
      not String.contains?(value, [<<0>>, "\\", "\r", "\n", "\t", "*", "?", "[", "]"]) and
      Enum.all?(String.split(value, "/"), &(&1 not in ["", ".", "..", ".git"]))
  end

  defp path?(_), do: false

  defp keys!(value, required, optional \\ []) do
    valid!(is_map(value) and not is_struct(value))
    keys = Map.keys(value)

    valid!(
      Enum.all?(required, &Map.has_key?(value, &1)) and
        Enum.all?(keys, &(&1 in (required ++ optional)))
    )
  end

  defp proper_list?([]), do: true
  defp proper_list?([_ | rest]), do: proper_list?(rest)
  defp proper_list?(_), do: false
  defp valid!(true), do: :ok
  defp valid!(_), do: throw(:invalid_request)
end
