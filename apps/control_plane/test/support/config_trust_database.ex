# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.ConfigTrustDatabase do
  def open!(path) do
    pid = WorkHarbor.HostDatabase.open!(path)

    unless Code.ensure_loaded?(WorkHarbor.Repo.Migrations.RevisionTrust) do
      Code.require_file(
        Application.app_dir(:workharbor, "priv/repo/migrations/20261015000000_revision_trust.exs")
      )
    end

    Ecto.Migrator.run(
      WorkHarbor.Repo,
      [{20_261_015_000_000, WorkHarbor.Repo.Migrations.RevisionTrust}],
      :up,
      all: true,
      log: false
    )

    pid
  end
end
