# SPDX-License-Identifier: EUPL-1.2

defmodule Mix.Tasks.Workharbor.Hosts.Init do
  @shortdoc "Explicitly initialize the private client certificate issuer"
  use Mix.Task
  @impl true
  def run(["--issuer-dir", path]) do
    Mix.Task.run("app.config")
    {:ok, _} = Application.ensure_all_started(:public_key)

    case WorkHarbor.Hosts.Issuer.initialize(path) do
      {:ok, _} -> Mix.shell().info("Client issuer initialized")
      _ -> Mix.raise("Client issuer initialization refused")
    end
  end

  def run(_), do: Mix.raise("Usage: mix workharbor.hosts.init --issuer-dir PATH")
end
