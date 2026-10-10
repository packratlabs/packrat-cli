module ErrorContext
  def self.set(user)
    return unless Sentry.initialized? && user

    Sentry.set_user(id: user.id)
  end
end
