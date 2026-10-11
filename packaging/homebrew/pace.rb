# frozen_string_literal: true

# The release workflow's homebrew job fills in @VERSION@ and the SHA-256 of each
# archive and writes the result to Casks/pace.rb in soundadam/homebrew-tap.
cask "pace" do
  version "@VERSION@"

  on_macos do
    on_arm do
      sha256 "@MACOS_AARCH64_SHA256@"

      url "https://github.com/soundadam/pace/releases/download/v#{version}/pace-#{version}-macos-aarch64.tar.gz"

      binary "pace-#{version}-macos-aarch64/pace"
    end
    on_intel do
      sha256 "@MACOS_X86_64_SHA256@"

      url "https://github.com/soundadam/pace/releases/download/v#{version}/pace-#{version}-macos-x86_64.tar.gz"

      binary "pace-#{version}-macos-x86_64/pace"
    end

    # Ad-hoc signed and not notarized: clear the quarantine bit so Gatekeeper
    # treats it like a file that pace update put in place.
    postflight_steps do
      run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{staged_path}}"]
    end
  end
  on_linux do
    on_arm do
      sha256 "@LINUX_AARCH64_SHA256@"

      url "https://github.com/soundadam/pace/releases/download/v#{version}/pace-#{version}-linux-aarch64.tar.gz"

      binary "pace-#{version}-linux-aarch64/pace"
    end
    on_intel do
      sha256 "@LINUX_X86_64_SHA256@"

      url "https://github.com/soundadam/pace/releases/download/v#{version}/pace-#{version}-linux-x86_64.tar.gz"

      binary "pace-#{version}-linux-x86_64/pace"
    end
  end

  name "pace"
  desc "Speed tests for proxies, campus intranets and CERNET"
  homepage "https://soundadam.com/projects/pace/"

  livecheck do
    url :url
    strategy :github_latest
  end

  caveats <<~EOS
    pace is MIT licensed.

    The optional measurement helpers are separate programs with their own
    licenses and are NOT bundled with this package:
      - librespeed-cli (LGPL-3.0-only)
      - ndt7-client (Apache-2.0)
    pace never downloads them automatically. Run `pace doctor`
    to see which helpers are available on this machine.
  EOS
end
