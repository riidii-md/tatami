class Tatami < Formula
  desc "Terminal workspace manager with Zellij/Tmux integration"
  homepage "https://github.com/OleksandrBesan/tatami"
  version "0.2.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/OleksandrBesan/tatami/releases/download/v#{version}/tatami_#{version}_darwin_arm64.tar.gz"
      sha256 "2a3ef5c9fe6998b305474b6707ba4822a9f57a46be086ef5f06d9c0469a0311a"
    else
      url "https://github.com/OleksandrBesan/tatami/releases/download/v#{version}/tatami_#{version}_darwin_amd64.tar.gz"
      sha256 "5ec7ff9fb56d0a817f4c344e78aa7ddcc75b46eedbc61be328e185848aeed0da"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/OleksandrBesan/tatami/releases/download/v#{version}/tatami_#{version}_linux_arm64.tar.gz"
      sha256 "894b5e8c2dfb5a92b7672d34194160e7cbc696c48b61f82eabd45a523af7055b"
    else
      url "https://github.com/OleksandrBesan/tatami/releases/download/v#{version}/tatami_#{version}_linux_amd64.tar.gz"
      sha256 "de59646952e6b9758092799c7484a57ffaccca96729ac6e3cf442070d4b9a746"
    end
  end

  def install
    bin.install "tatami"
  end

  def caveats
    <<~EOS
      To enable automatic cd, add to your ~/.zshrc or ~/.bashrc:

        tatami() {
          local output
          output=$(TATAMI_WRAPPER=1 command tatami "$@")
          local exit_code=$?
          if [[ $exit_code -eq 0 && -d "$output" ]]; then
            cd "$output"
          elif [[ -n "$output" ]]; then
            echo "$output"
          fi
          return $exit_code
        }
    EOS
  end

  test do
    system "#{bin}/tatami", "--version"
  end
end
