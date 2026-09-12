
import './students.css'

function Students() {
  return (
    <div className="students-page">

      <div className="students-header">
        <div>
          <h1>Пользователи</h1>
          <p>Ученики и преподаватели</p>
        </div>

        <div className="students-actions">
          <button className="secondary-button">
            Отсканировать список
          </button>

          <button className="secondary-button">
            Скачать список
          </button>

          <button className="primary-button">
            + Добавить пользователя
          </button>
        </div>
      </div>

      <div className="students-list">

        <div className="student-row">
          <div className="student-main">
            <div className="student-avatar">
              👨‍🎓
            </div>

            <div className="student-info">
              <div className="student-name">
                Иван Иванов
                <span className="student-id">#123456</span>
              </div>

              <div className="student-role">
                Ученик
              </div>
            </div>
          </div>

          <div className="student-status">
            <span className="status-dot online"></span>
            <span>В сети</span>
          </div>

          <button className="student-menu-button">
            ⋮
          </button>
        </div>

        <div className="student-row">
          <div className="student-main">
            <div className="student-avatar">
              👩‍🏫
            </div>

            <div className="student-info">
              <div className="student-name">
                Анна Петрова
                <span className="student-id">#849201</span>
              </div>

              <div className="student-role">
                Учитель
              </div>
            </div>
          </div>

          <div className="student-status">
            <span className="status-dot offline"></span>
            <span>Не в сети</span>
          </div>

          <button className="student-menu-button">
            ⋮
          </button>
        </div>

      </div>

    </div>
  )
}

export default Students

