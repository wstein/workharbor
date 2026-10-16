# SPDX-License-Identifier: EUPL-1.2

defmodule WorkHarbor.Hosts.TrustedIssuers do
  @moduledoc """
  Client-CA set for the agent listener, following issuer rotation without a restart.

  Rotation replaces the issuer state file. The set is the current issuer plus every
  earlier issuer seen by this process that has not yet expired, so certificates issued
  before the rotation keep working for their remaining validity. A replacement that fails
  `Issuer.load/1` never widens or shrinks trust. The set is process-local: after a restart
  only the then-current issuer is known. An issuer is remembered once observed here;
  `Hosts.issuer/0` observes every issuer that signs an enrollment or renewal.
  """
  alias WorkHarbor.Hosts.Issuer

  # A failing file is retried after this long; trust is unchanged meanwhile.
  @backoff_ms 5_000

  # H76:a. A replaced issuer stays trusted for the maximum leaf lifetime plus this margin
  # (seconds), counted from when this process first observed the replacement.
  @retired_margin 3_600

  def issuers(dir) when is_binary(dir) do
    now = WorkHarbor.Accounts.now()
    key = {__MODULE__, dir}
    cached = :persistent_term.get(key, nil)
    stamp = stamp(dir)

    state =
      if fresh?(cached, stamp) do
        cached
      else
        # The lock only serialises refreshes; the hot path above takes none.
        :global.trans({key, self()}, fn ->
          latest = refresh(dir, :persistent_term.get(key, nil), stamp, now)
          :persistent_term.put(key, latest)
          latest
        end)
      end

    live(state.issuers, now)
  end

  defp fresh?(nil, _), do: false

  defp fresh?(cached, stamp) do
    (stamp != nil and cached.stamp == stamp) or
      case cached.failed do
        {^stamp, until} -> until > System.monotonic_time(:millisecond)
        _ -> false
      end
  end

  defp refresh(dir, cached, stamp, now) do
    cached = cached || %{stamp: nil, issuers: [], failed: nil}

    if fresh?(cached, stamp) do
      cached
    else
      case if(stamp, do: Issuer.load(dir)) do
        {:ok, current} ->
          previous =
            cached.issuers
            |> Enum.reject(&(&1.certificate == current.certificate))
            |> Enum.map(&Map.put_new(&1, :retired_at, now))

          %{
            stamp: stamp,
            issuers: live([Map.delete(current, :retired_at) | previous], now),
            failed: nil
          }

        _ ->
          # Unreadable, malformed, unsafe or invalid replacement: trust is unchanged and the
          # failure is remembered so unauthenticated handshakes cannot queue on the lock.
          %{cached | failed: {stamp, System.monotonic_time(:millisecond) + @backoff_ms}}
      end
    end
  end

  defp live(issuers, now) do
    window = Issuer.leaf_lifetime() + @retired_margin

    Enum.filter(issuers, fn issuer ->
      issuer.expires_at > now and now < Map.get(issuer, :retired_at, now) + window
    end)
  end

  # Content digest, not mtime/inode: same-second replacements must not be missed.
  defp stamp(dir) do
    case File.read(Path.join(dir, "issuer.json")) do
      {:ok, bytes} when byte_size(bytes) <= 16_384 -> :crypto.hash(:sha256, bytes)
      _ -> nil
    end
  end

  @doc """
  ssl verify_fun. The static boot root verifies normally. For an unknown CA, ssl calls this
  with the TOP certificate of the chain and a valid result makes it the trust anchor, with no
  CA, key-usage or path-length checks. So the anchor is accepted only as either a set member
  (DER equality; the chain carries the issuer) or a complete end-entity leaf of a member, and
  the latter admits single-certificate chains only: every later event fails.
  """
  def verify(cert, {:bad_cert, :unknown_ca} = event, dir) when is_binary(dir) do
    case anchor(cert, dir) do
      nil -> {:fail, event}
      kind -> {:valid, {dir, kind}}
    end
  end

  def verify(_cert, {:bad_cert, _} = event, _state), do: {:fail, event}
  def verify(_cert, _event, {_dir, :leaf}), do: {:fail, :leaf_anchor_cannot_issue}
  def verify(_cert, {:extension, _}, state), do: {:unknown, state}
  def verify(_cert, :valid, state), do: {:valid, state}
  def verify(_cert, :valid_peer, state), do: {:valid, state}

  defp anchor(cert, dir) do
    der = :public_key.pkix_encode(:OTPCertificate, cert, :otp)
    now = WorkHarbor.Accounts.now()
    issuers = issuers(dir)

    cond do
      Enum.any?(issuers, &(&1.certificate == der)) -> :member
      Enum.any?(issuers, &Issuer.valid_leaf_profile?(der, &1, now)) -> :leaf
      true -> nil
    end
  rescue
    _ -> nil
  catch
    _ -> nil
  end
end
