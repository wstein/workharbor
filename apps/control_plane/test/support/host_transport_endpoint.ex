# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostTransportEndpoint do
  @moduledoc "Test-only committed fixture repository wiring; production ingress is unchanged."
  def init(opts), do: opts

  def call(conn, _) do
    WorkHarbor.Repo.put_dynamic_repo(
      Application.fetch_env!(:workharbor, :host_transport_fixture_repo)
    )

    WorkHarborWeb.Endpoint.call(conn, WorkHarborWeb.Endpoint.init([]))
  end
end
