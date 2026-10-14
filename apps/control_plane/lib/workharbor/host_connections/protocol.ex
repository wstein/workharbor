# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostConnections.Protocol do
  @moduledoc false
  alias WorkHarbor.{StrictJSON}
  alias WorkHarbor.Foundation.{Request, Canonical}

  def decode(raw, owner, host) when is_binary(raw) and byte_size(raw) <= 4096 do
    with {:ok, value} <- StrictJSON.decode_object(raw), do: validate(value, owner, host)
  end

  def decode(_, _, _), do: {:error, :invalid_connection_message}

  def validate(value, owner, host) do
    keys =
      if is_map(value) and value["kind"] == "hello",
        do: ~w(schema_version kind owner_id host_id capabilities),
        else: ~w(schema_version kind owner_id host_id)

    valid =
      is_map(value) and not is_struct(value) and Enum.sort(Map.keys(value)) == Enum.sort(keys) and
        value["schema_version"] === 1 and
        value["kind"] in ~w(hello hello_ack heartbeat heartbeat_ack) and Request.uuid?(owner) and
        Request.uuid?(host) and
        value["owner_id"] === owner and value["host_id"] === host and
        (value["kind"] != "hello" or value["capabilities"] in [[], ["docker"]])

    if valid do
      case Canonical.bytes(value) do
        {:ok, bytes} when byte_size(bytes) <= 4096 -> {:ok, value}
        _ -> {:error, :invalid_connection_message}
      end
    else
      {:error, :invalid_connection_message}
    end
  end
end
