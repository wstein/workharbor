# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostLogCapture do
  @moduledoc "Test handler receives events only after all native primary filters."
  def adding_handler(config), do: {:ok, config}
  def removing_handler(_), do: :ok
  def changing_config(_, _, config), do: {:ok, config}
  def log(event, %{config: %{test: test}}), do: send(test, {:after_primary_log, event})
end
