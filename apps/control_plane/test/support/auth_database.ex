# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.AuthDatabase do
  def open!(path) do
    pid = WorkHarbor.FoundationDatabase.open!(path)

    Code.require_file(
      Application.app_dir(
        :workharbor,
        "priv/repo/migrations/20261014010000_owner_authentication.exs"
      )
    )

    Ecto.Migrator.run(
      WorkHarbor.Repo,
      [{20_261_014_010_000, WorkHarbor.Repo.Migrations.OwnerAuthentication}],
      :up,
      all: true,
      log: false
    )

    pid
  end

  def fixture! do
    {:ok, owner} =
      WorkHarbor.Accounts.bootstrap(fn secret ->
        send(self(), {:bootstrap_secret, secret})
        :ok
      end)

    receive do
      {:bootstrap_secret, secret} -> {owner.id, secret}
    end
  end
end
