defmodule WorkHarbor.Repo do
  use Ecto.Repo,
    otp_app: :workharbor,
    adapter: Ecto.Adapters.SQLite3
end
