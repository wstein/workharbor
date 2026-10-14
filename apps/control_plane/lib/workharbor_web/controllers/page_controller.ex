defmodule WorkHarborWeb.PageController do
  use WorkHarborWeb, :controller

  def home(conn, _params) do
    render(conn, :home)
  end
end
