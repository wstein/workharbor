# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.HostDatabase do
  def open!(path) do
    pid = WorkHarbor.AuthDatabase.open!(path)

    unless Code.ensure_loaded?(WorkHarbor.Repo.Migrations.HostIdentity) do
      Code.require_file(
        Application.app_dir(:workharbor, "priv/repo/migrations/20261014020000_host_identity.exs")
      )
    end

    Ecto.Migrator.run(
      WorkHarbor.Repo,
      [{20_261_014_020_000, WorkHarbor.Repo.Migrations.HostIdentity}],
      :up,
      all: true,
      log: false
    )

    pid
  end
end
