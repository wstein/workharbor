defmodule WorkHarborWeb.PageControllerTest do
  use WorkHarborWeb.ConnCase

  test "GET /", %{conn: conn} do
    conn = get(conn, ~p"/")
    assert json_response(conn, 403) == %{"error" => %{"code" => "forbidden"}}
  end
end
