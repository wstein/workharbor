# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Accounts.Authenticator do
  use Ecto.Schema
  @primary_key {:id, :integer, autogenerate: false}
  schema "owner_authenticators" do
    field :owner_id, :binary_id
    field :secret_hash, :binary, redact: true
  end
end

defmodule WorkHarbor.Accounts.Credential do
  use Ecto.Schema
  @primary_key {:id, :binary_id, autogenerate: true}
  schema "owner_credentials" do
    field :owner_id, :binary_id
    field :token_hash, :binary, redact: true
    field :kind, :string
    field :issued_at, :integer
    field :expires_at, :integer
    field :revoked_at, :integer
  end
end

defmodule WorkHarbor.Accounts.Principal do
  @moduledoc "Trusted server-created identity, separate from request bodies."
  @enforce_keys [:owner_id, :credential_id, :kind, :scopes, :issued_at, :expires_at]
  defstruct @enforce_keys
end

defmodule WorkHarbor.Accounts do
  @moduledoc "Single-owner primary authentication and revocable opaque credentials."
  alias WorkHarbor.Repo
  alias WorkHarbor.Foundation.{Owner, AuditEvent, Request}
  alias WorkHarbor.Accounts.{Authenticator, Credential, Principal}
  @kinds ["cli_owner", "owner_browser", "dashboard"]
  @writes [
    :logout,
    :revoke,
    :issue_dashboard,
    :primary_login,
    :host_enrollment_issue,
    :host_revoke,
    :config_approve
  ]
  @reads [:status, :owner_page, :dashboard_page, :credential_list, :host_status, :config_read]
  def now, do: Application.get_env(:workharbor, :clock, fn -> System.system_time(:second) end).()

  def bootstrap(deliver, owner_id \\ nil) when is_function(deliver, 1) do
    result =
      transaction(fn ->
        if Repo.get(Authenticator, 1) do
          {:error, :already_initialized}
        else
          owner =
            if is_nil(owner_id),
              do: Repo.insert!(%Owner{display_name: "Owner"}, log: false),
              else: if(Request.uuid?(owner_id), do: Repo.get(Owner, owner_id))

          if is_nil(owner) do
            {:error, :invalid_request}
          else
            if is_nil(owner_id),
              do:
                audit(
                  owner.id,
                  nil,
                  "owner_created",
                  "owner_metadata",
                  "internal",
                  "recorded",
                  "ok"
                )

            secret = random()

            Repo.insert!(
              %Authenticator{id: 1, owner_id: owner.id, secret_hash: digest(:primary, secret)},
              log: false
            )

            audit(owner.id, nil, "owner_bootstrapped", "bootstrap", "internal", "recorded", "ok")
            {:ok, owner, secret}
          end
        end
      end)

    case result do
      {:ok, owner, secret} ->
        try do
          if deliver.(secret) == :ok, do: {:ok, owner}, else: {:error, :delivery_failed}
        rescue
          _ -> {:error, :delivery_failed}
        catch
          _, _ -> {:error, :delivery_failed}
        end

      other ->
        other
    end
  end

  def login(owner, secret, kind) when kind in @kinds do
    verifier = if Request.uuid?(owner), do: Repo.get_by(Authenticator, owner_id: owner)
    expected = if verifier, do: verifier.secret_hash, else: <<0::256>>
    candidate = digest(:primary, if(is_binary(secret), do: secret, else: ""))
    valid = Plug.Crypto.secure_compare(expected, candidate)

    if verifier && valid && canonical?(secret, 43) do
      transaction(fn -> issue!(owner, kind) end)
    else
      {:error, :unauthenticated}
    end
  end

  def login(_, _, _), do: {:error, :invalid_request}

  def authenticate(token, expected_kind \\ :any) do
    with true <- token?(token),
         %Credential{} = c <- Repo.get_by(Credential, token_hash: digest(:credential, token)),
         true <- active?(c) and (expected_kind == :any or c.kind == expected_kind) do
      {:ok, principal(c)}
    else
      _ -> {:error, :unauthenticated}
    end
  end

  def authorize(p, action, owner \\ nil)

  def authorize(%Principal{} = p, action, owner) do
    current = Repo.get(Credential, p.credential_id)
    owner = owner || p.owner_id

    valid =
      current && active?(current) && current.owner_id == p.owner_id && current.kind == p.kind

    allowed = valid && owner == p.owner_id && action_allowed?(current.kind, action)

    if allowed do
      :ok
    else
      if current && current.owner_id == p.owner_id,
        do:
          audit(
            p.owner_id,
            p.credential_id,
            "authorization_denied",
            safe_action(action),
            channel(p.kind),
            "denied",
            "forbidden"
          )

      {:error, :forbidden}
    end
  end

  def authorize(_, _, _), do: {:error, :forbidden}

  def deny(%Principal{} = p, action) do
    audit(
      p.owner_id,
      p.credential_id,
      "authorization_denied",
      safe_action(action),
      channel(p.kind),
      "denied",
      "forbidden"
    )

    {:error, :forbidden}
  end

  def issue_dashboard(p) do
    transaction(fn ->
      with :ok <- authorize(p, :issue_dashboard), do: issue!(p.owner_id, "dashboard")
    end)
  end

  def revoke(p, id) do
    transaction(fn ->
      with :ok <- authorize(p, :revoke),
           true <- Request.uuid?(id),
           %Credential{} = c <- Repo.get_by(Credential, id: id, owner_id: p.owner_id) do
        if is_nil(c.revoked_at),
          do: c |> Ecto.Changeset.change(revoked_at: now()) |> Repo.update!(log: false)

        audit(
          p.owner_id,
          nil,
          "credential_revoked",
          "revoke",
          channel(p.kind),
          "recorded",
          "ok",
          %{"actor_credential_id" => p.credential_id, "target_credential_id" => c.id}
        )

        :ok
      else
        {:error, _} = e ->
          e

        _ ->
          audit(
            p.owner_id,
            p.credential_id,
            "authorization_denied",
            "revoke",
            channel(p.kind),
            "denied",
            "not_found"
          )

          {:error, :not_found}
      end
    end)
  end

  def status(%Principal{} = p), do: Map.from_struct(p)
  def token?(<<"whr1_", encoded::binary>>), do: canonical?(encoded, 43)
  def token?(_), do: false
  def primary?(secret), do: canonical?(secret, 43)

  defp canonical?(value, size) when is_binary(value) and byte_size(value) == size do
    case Base.url_decode64(value, padding: false) do
      {:ok, raw} -> byte_size(raw) == 32 and Base.url_encode64(raw, padding: false) == value
      _ -> false
    end
  end

  defp canonical?(_, _), do: false

  defp issue!(owner, kind) do
    token = "whr1_" <> random()
    time = now()

    c =
      Repo.insert!(
        %Credential{
          owner_id: owner,
          token_hash: digest(:credential, token),
          kind: kind,
          issued_at: time,
          expires_at: time + if(kind == "cli_owner", do: 2_592_000, else: 28_800)
        },
        log: false
      )

    audit(owner, c.id, "credential_issued", "issue", channel(kind), "recorded", "ok")
    {:ok, token, principal(c)}
  end

  defp active?(c), do: is_nil(c.revoked_at) and c.expires_at > now() and c.issued_at <= now()

  defp principal(c),
    do: %Principal{
      owner_id: c.owner_id,
      credential_id: c.id,
      kind: c.kind,
      scopes:
        if(c.kind == "dashboard", do: ["dashboard:read"], else: ["owner:read", "owner:write"]),
      issued_at: c.issued_at,
      expires_at: c.expires_at
    }

  defp action_allowed?(kind, a) when a in @writes, do: kind in ["cli_owner", "owner_browser"]
  defp action_allowed?(kind, :owner_page), do: kind == "owner_browser"
  defp action_allowed?(kind, :dashboard_page), do: kind == "dashboard"
  defp action_allowed?(kind, :host_status), do: kind in ["cli_owner", "owner_browser"]
  defp action_allowed?(_, a) when a in [:status, :credential_list, :config_read], do: true
  defp action_allowed?(_, _), do: false
  defp safe_action(a) when a in @writes or a in @reads, do: Atom.to_string(a)
  defp safe_action(_), do: "unknown"
  defp channel("cli_owner"), do: "cli"
  defp channel(_), do: "browser"

  defp audit(owner, credential, event, action, channel, outcome, code, extra \\ %{}) do
    Repo.insert!(
      %AuditEvent{
        owner_id: owner,
        event: event,
        action: action,
        channel: channel,
        outcome: outcome,
        code: code,
        metadata:
          Map.merge(if(credential, do: %{"credential_id" => credential}, else: %{}), extra)
      },
      log: false
    )
  end

  defp digest(:primary, v), do: :crypto.hash(:sha256, "WorkHarbor primary v1\0" <> v)
  defp digest(:credential, v), do: :crypto.hash(:sha256, "WorkHarbor credential v1\0" <> v)
  defp random, do: :crypto.strong_rand_bytes(32) |> Base.url_encode64(padding: false)

  defp transaction(work) do
    if Repo.in_transaction?() do
      {:error, :nested_transaction}
    else
      case Repo.transaction(work, mode: :immediate, log: false) do
        {:ok, result} -> result
        {:error, _} -> {:error, :persistence_failed}
      end
    end
  rescue
    _ in [Exqlite.Error, DBConnection.ConnectionError, Ecto.ConstraintError] ->
      {:error, :persistence_failed}
  end
end
